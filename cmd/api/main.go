package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/odiegodev1/ticketfy-payments/internal/cajupay"
	"github.com/odiegodev1/ticketfy-payments/internal/config"
	"github.com/odiegodev1/ticketfy-payments/internal/database"
	"github.com/odiegodev1/ticketfy-payments/internal/payment"
)

func main() {

	// ==========================================
	// 1. CARREGAR CONFIGURAÇÕES
	// ==========================================

	cfg, err := config.Load()
	if err != nil {
		log.Fatal("configuração: ", err)
	}

	// ==========================================
	// 2. CONTEXTO PRINCIPAL
	// ==========================================

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	// ==========================================
	// 3. CONECTAR AO NEONDB
	// ==========================================

	dbCtx, cancelDB := context.WithTimeout(
		ctx,
		10*time.Second,
	)

	pool, err := database.Connect(
		dbCtx,
		cfg.DatabaseURL,
	)

	cancelDB()

	if err != nil {
		log.Fatal("conexão com banco: ", err)
	}

	defer pool.Close()

	repo := payment.Repository{
		DB: pool,
	}

	log.Println("[DATABASE] Conexão com NeonDB estabelecida")

	// ==========================================
	// 4. CONFIGURAR GATEWAY DE PAGAMENTO
	// ==========================================

	var gateway payment.PixGateway
	var cajuClient *cajupay.Client

	switch cfg.PaymentGateway {

	case "mock":

		gateway = payment.MockPixGateway{}

		log.Println(
			"[PAYMENT] Gateway: MOCK (simulação local)",
		)

	case "cajupay":

		client, err := cajupay.NewClient(
			cfg.CajuPayBaseURL,
			cfg.CajuPayAPIKey,
			cfg.CajuPayAPISecret,
		)

		if err != nil {
			log.Fatal("cliente CajuPay: ", err)
		}

		cajuClient = client

		gateway = payment.CajuPayGateway{
			Client:      client,
			CheckoutURL: cfg.PartnerCheckoutURL,
		}

		log.Println(
			"[PAYMENT] Gateway: CAJUPAY",
		)

	default:

		log.Fatal(
			"gateway de pagamento não suportado: ",
			cfg.PaymentGateway,
		)
	}

	// ==========================================
	// 5. CONFIGURAR SERVIÇOS DE PAGAMENTO
	// ==========================================

	pixService := payment.PixService{
		Repo:    repo,
		Gateway: gateway,
	}

	handler := payment.Handler{
		Repo: repo,
		Pix:  pixService,
	}

	// ==========================================
	// 6. CONFIGURAR WEBHOOK SERVICE
	// ==========================================

	webhookService := payment.WebhookService{
		Repo:   repo,
		Client: cajuClient,
	}

	webhookHandler := payment.WebhookHandler{
		Service: webhookService,
	}

	// ==========================================
	// 7. CONFIGURAR CONFIRMATION SERVICE
	// ==========================================

	confirmationService := payment.ConfirmationService{
		Webhook: webhookService,
		Repo:    repo,
	}

	confirmationHandler := payment.ConfirmationHandler{
		Service: confirmationService,
	}

	// ==========================================
	// 8. WORKER DE RECONCILIAÇÃO
	// ==========================================

	reconciliationWorker := payment.Worker{
		Repo:     repo,
		Client:   cajuClient,
		Interval: 2 * time.Minute,
	}

	go reconciliationWorker.Start(ctx)

	log.Println(
		"[WORKER] Reconciliação de pagamentos inicializada",
	)

	// ==========================================
	// 9. WORKER DE CONFIRMAÇÃO AUTOMÁTICA PIX
	// ==========================================

	if cajuClient != nil {

		paymentConfirmationWorker := payment.PaymentConfirmationWorker{
			Repo:      repo,
			Service:   confirmationService,
			Interval:  30 * time.Second,
			BatchSize: 50,
		}

		go paymentConfirmationWorker.Start(ctx)

		log.Println(
			"[CONFIRMATION WORKER] Confirmação automática habilitada",
		)

	} else {

		log.Println(
			"[CONFIRMATION WORKER] Desativado: CajuPay não configurada",
		)
	}

	// ==========================================
	// 10. WORKER DE ENTREGA GO -> JAVA
	// ==========================================

	javaURL := strings.TrimSpace(
		os.Getenv("TICKETFY_JAVA_URL"),
	)

	if javaURL != "" {

		if cajuClient == nil {

			log.Println(
				"[OUTBOX] Desativado: gateway CajuPay não configurado",
			)

		} else {

			outboxWorker := payment.OutboxWorker{
				Repo:     repo,
				JavaURL:  javaURL,
				Token:    cfg.InternalToken,
				Interval: 10 * time.Second,

				HTTPClient: &http.Client{
					Timeout: 8 * time.Second,
				},
			}

			go outboxWorker.Start(ctx)

			log.Println(
				"[OUTBOX] Worker de entrega Go -> Java inicializado",
			)
		}

	} else {

		log.Println(
			"[OUTBOX] Desativado: TICKETFY_JAVA_URL não configurada",
		)
	}

	// ==========================================
	// 11. CONFIGURAR ROTAS HTTP
	// ==========================================

	r := chi.NewRouter()

	// ==========================================
	// HEALTH CHECK
	// ==========================================

	r.Get("/health", func(
		w http.ResponseWriter,
		r *http.Request,
	) {

		w.Header().Set(
			"Content-Type",
			"application/json",
		)

		w.WriteHeader(http.StatusOK)

		_ = json.NewEncoder(w).Encode(
			map[string]string{
				"status":  "ok",
				"service": "ticketfy-payments",
			},
		)
	})

	// ==========================================
	// ROTAS INTERNAS PROTEGIDAS
	// ==========================================

	r.Route("/internal", func(r chi.Router) {

		// ======================================
		// MIDDLEWARE DE AUTENTICAÇÃO INTERNA
		// ======================================

		r.Use(func(next http.Handler) http.Handler {

			return http.HandlerFunc(func(
				w http.ResponseWriter,
				req *http.Request,
			) {

				authorization := req.Header.Get(
					"Authorization",
				)

				const prefix = "Bearer "

				if !strings.HasPrefix(
					authorization,
					prefix,
				) {

					http.Error(
						w,
						"unauthorized",
						http.StatusUnauthorized,
					)

					return
				}

				suppliedToken := strings.TrimPrefix(
					authorization,
					prefix,
				)

				if cfg.InternalToken == "" ||
					subtle.ConstantTimeCompare(
						[]byte(suppliedToken),
						[]byte(cfg.InternalToken),
					) != 1 {

					http.Error(
						w,
						"unauthorized",
						http.StatusUnauthorized,
					)

					return
				}

				next.ServeHTTP(w, req)
			})
		})

		// ======================================
		// CRIAR PAGAMENTO
		// ======================================

		r.Post(
			"/payments",
			handler.Create,
		)

		// ======================================
		// CONSULTAR PAGAMENTO
		// ======================================

		r.Get(
			"/payments/{id}",
			handler.Get,
		)

		// ======================================
		// CRIAR PIX
		// ======================================

		r.Post(
			"/payments/{id}/pix",
			handler.CreatePix,
		)

		// ======================================
		// VERIFICAR PAGAMENTO NA CAJUPAY
		// ======================================

		r.Post(
			"/payments/verify",
			webhookHandler.Verify,
		)

		// ======================================
		// CONFIRMAR PAGAMENTO
		// ======================================

		r.Post(
			"/payments/{id}/confirm",
			confirmationHandler.Confirm,
		)
	})

	// ==========================================
	// 12. CONFIGURAR SERVIDOR HTTP
	// ==========================================

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// ==========================================
	// 13. INICIAR SERVIDOR
	// ==========================================

	serverErrors := make(chan error, 1)

	go func() {

		log.Println(
			"[API] Ticketfy Payments iniciado em",
			cfg.Addr,
		)

		err := server.ListenAndServe()

		if err != nil &&
			!errors.Is(err, http.ErrServerClosed) {

			serverErrors <- err
		}
	}()

	// ==========================================
	// 14. AGUARDAR ENCERRAMENTO
	// ==========================================

	select {

	case <-ctx.Done():

		log.Println(
			"[API] Encerramento solicitado",
		)

	case err := <-serverErrors:

		log.Printf(
			"[API] Erro no servidor: %v",
			err,
		)

		stop()
	}

	// ==========================================
	// 15. ENCERRAMENTO SEGURO
	// ==========================================

	shutdownCtx, cancelShutdown := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)

	defer cancelShutdown()

	if err := server.Shutdown(shutdownCtx); err != nil {

		log.Printf(
			"[API] Erro ao encerrar servidor: %v",
			err,
		)
	}

	log.Println(
		"[API] Ticketfy Payments encerrado",
	)
}
