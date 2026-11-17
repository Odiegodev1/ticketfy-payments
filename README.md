<div align="center">

# 🎟️ Ticketfy Payments

### Microserviço de pagamentos Pix com Go, PostgreSQL e CajuPay

**Pagamentos desacoplados • Idempotência • Reconciliação • Integração com Ticketfy API**

![Go](https://img.shields.io/badge/Go-Backend-00ADD8?logo=go&logoColor=white)
![PostgreSQL](https://img.shields.io/badge/PostgreSQL-Database-4169E1?logo=postgresql&logoColor=white)
![Pix](https://img.shields.io/badge/Pix-Pagamentos-32BCAD)
![Status](https://img.shields.io/badge/Status-Em%20desenvolvimento-orange)

</div>

---

## ✨ Sobre o projeto

O **Ticketfy Payments** é o serviço responsável pelo ciclo de vida de pagamentos da plataforma Ticketfy. Desenvolvido em **Go**, integra a API principal, construída em **Java / Spring Boot**, ao provedor **CajuPay** para criação e acompanhamento de cobranças Pix.

O serviço mantém o estado dos pagamentos em **PostgreSQL**, utiliza identificadores para evitar cobranças duplicadas e conta com mecanismos de recuperação e reconciliação para lidar com falhas de rede ou interrupções de processamento.

> **Regra essencial:** criar uma cobrança Pix **não** significa que ela foi paga. A emissão de ingressos deve ocorrer somente depois de uma confirmação de pagamento validada pelo backend.

## 🧩 Tecnologias

| Tecnologia | Utilização |
|---|---|
| **Go** | API HTTP e lógica de pagamentos |
| **PostgreSQL / Neon** | Persistência de pagamentos e eventos de entrega |
| **CajuPay** | Provedor de pagamentos Pix |
| **Java / Spring Boot** | API principal da Ticketfy, consumidora do serviço |
| **Docker / Render** | Empacotamento e hospedagem, conforme configuração do ambiente |

## 🏗️ Arquitetura

```mermaid
flowchart TD
    C[Cliente / Checkout] --> J[Ticketfy API · Java]
    J -->|Solicitação autenticada| G[Ticketfy Payments · Go]
    G --> D[(PostgreSQL / Neon)]
    G -->|Criar cobrança Pix| P[CajuPay]
    P -->|Estado do pagamento| G
    G -->|Confirmação validada / evento| J
    J -->|Pagamento confirmado| T[Emissão de ingressos]
```

**Responsabilidades:** a API Java gerencia pedidos e ingressos; o serviço Go gerencia a cobrança e seus estados. A confirmação de pagamento deve ser validada antes de alterar um pedido para `PAID`.

## 🔄 Fluxo de pagamento

1. A Ticketfy API cria um pedido com status `PENDING`.
2. A API solicita a criação de um registro de pagamento ao serviço Go.
3. O serviço Go reserva a operação e solicita um Pix à CajuPay.
4. O cliente recebe as informações de pagamento.
5. O serviço consulta ou recebe informações de atualização do provedor.
6. **Somente após confirmação confiável**, o pagamento pode passar para `PAID`.
7. A API principal recebe a confirmação por seu fluxo interno e emite os ingressos.

### Estados de pagamento

| Estado | Significado |
|---|---|
| `PENDING` | Aguardando confirmação |
| `PAID` | Pagamento confirmado |
| `FAILED` | Falha no pagamento |
| `CANCELLED` | Pagamento cancelado |

A criação do Pix possui controle próprio, incluindo `NOT_STARTED` e `CREATED` no modelo observado durante o desenvolvimento.

## 📁 Organização do projeto

```text
ticketfy-payments/
├── cmd/
│   └── api/                 # Inicialização do servidor
├── internal/
│   ├── cajupay/             # Cliente do provedor de pagamento
│   ├── config/              # Leitura e validação das configurações
│   ├── database/            # Acesso ao PostgreSQL
│   └── payment/             # Domínio, handlers e serviços de pagamento
├── migrations/              # Evolução do esquema do banco
├── go.mod
└── go.sum
```

> A árvore apresenta os diretórios conhecidos do projeto. Consulte a branch atual para a lista completa de arquivos.

## 🚀 Executando localmente

### Pré-requisitos

- Go compatível com a versão indicada em `go.mod`.
- PostgreSQL acessível (local ou Neon).
- Credenciais da CajuPay **somente para o modo CajuPay**.

### 1. Clonar

```bash
git clone https://github.com/Odiegodev1/ticketfy-payments.git
cd ticketfy-payments
```

### 2. Configurar variáveis

Crie um arquivo `.env` local **sem enviá-lo ao Git**. Os nomes abaixo refletem as configurações utilizadas durante o desenvolvimento; confira `internal/config` para a lista definitiva.

```dotenv
PORT=8081
DATABASE_URL=postgresql://USUARIO:SENHA@HOST:5432/BANCO?sslmode=require
INTERNAL_API_TOKEN=defina-um-token-forte
PAYMENT_GATEWAY=MOCK

# Necessárias quando PAYMENT_GATEWAY=CAJUPAY
CAJUPAY_BASE_URL=https://URL-DO-AMBIENTE-DE-TESTES
CAJUPAY_API_KEY=sua-chave-de-teste
CAJUPAY_API_SECRET=seu-segredo-de-teste

# Configure se a integração com a API principal exigir
PARTNER_CHECKOUT_URL=https://URL-DA-API-PARCEIRA
```

**Observação:** não copie o endereço de exemplo da CajuPay como endpoint real. Utilize a URL do ambiente fornecida pelo provedor.

### 3. Preparar o banco

Aplique as migrations SQL existentes em `migrations/` na ordem correta, de acordo com a estratégia de migração do seu ambiente. Não execute scripts de criação em um banco de produção sem revisar seus efeitos.

### 4. Iniciar a API

```bash
go run ./cmd/api
```

A porta padrão usada no desenvolvimento foi **8081**.

## 📡 API HTTP

Rotas utilizadas no desenvolvimento do serviço:

| Método | Endpoint | Finalidade |
|---|---|---|
| `POST` | `/payments` | Criar registro de pagamento |
| `GET` | `/payments/{id}` | Consultar pagamento |
| `POST` | `/payments/{id}/pix` | Iniciar criação da cobrança Pix |

### Exemplo: criar pagamento

```bash
curl -X POST 'http://localhost:8081/payments' \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer SEU_TOKEN_INTERNO' \
  -d '{
    "orderId": "00000000-0000-0000-0000-000000000001",
    "amountCents": 28000
  }'
```

> **Atenção:** o cabeçalho de autenticação acima é ilustrativo. Confirme o formato exigido pelo middleware atual antes de copiar para uma integração real. O campo `amountCents` representa centavos: `28000` = **R$ 280,00**.

### Consultar pagamento

```bash
curl 'http://localhost:8081/payments/ID_DO_PAGAMENTO' \
  -H 'Authorization: Bearer SEU_TOKEN_INTERNO'
```

### Criar Pix

```bash
curl -X POST 'http://localhost:8081/payments/ID_DO_PAGAMENTO/pix' \
  -H 'Authorization: Bearer SEU_TOKEN_INTERNO'
```

Os contratos de resposta e a autenticação exata devem ser conferidos nos handlers antes de publicar uma documentação OpenAPI definitiva.

## 🛡️ Segurança e confiabilidade

- **Idempotência:** o fluxo de criação de Pix usa uma chave vinculada ao pagamento para reduzir o risco de operações duplicadas.
- **Estados explícitos:** pagamentos pendentes não são tratados como pagos.
- **Reconciliação:** há um worker de recuperação/consulta para pagamentos que exigem acompanhamento.
- **Timeouts:** chamadas externas possuem limites de tempo para evitar bloqueios indefinidos.
- **Persistência:** dados de pagamento são registrados em PostgreSQL.
- **Comunicação interna:** a integração entre serviços deve ser protegida por credenciais e restrições de acesso.
- **Segredos:** nunca versionar `.env`, tokens, chaves de API ou credenciais de banco.

> **Produção:** valide a autenticação dos webhooks, a entrega idempotente de confirmações e o comportamento de recuperação antes de habilitar pagamentos reais.

## 🧪 Testes e build

```bash
# Executa a suíte de testes
 go test ./...

# Compila o servidor
 go build -o ticketfy-payments ./cmd/api
```

Para evitar enviar executáveis compilados ao Git, adicione ao `.gitignore`:

```gitignore
.env
.env.*
!.env.example
/api
/app
/ticketfy-payments
```

## ☁️ Deploy no Render

Um caminho possível é criar um **Web Service** apontando para este repositório e configurar:

| Campo | Exemplo |
|---|---|
| Build Command | `go build -o server ./cmd/api` |
| Start Command | `./server` |
| Environment | `DATABASE_URL`, `INTERNAL_API_TOKEN`, `PAYMENT_GATEWAY` e credenciais necessárias |

O servidor deve escutar na porta indicada pela variável `PORT` do Render. Antes do deploy, verifique também a disponibilidade do banco, as migrations e a comunicação segura com a API Java.

## 🗺️ Evolução do projeto

- [x] Estrutura inicial em Go
- [x] Persistência de pagamentos em PostgreSQL
- [x] Integração com CajuPay em ambiente de testes
- [x] Fluxo de criação e consulta de pagamentos
- [x] Proteções de idempotência na criação do Pix
- [x] Worker de reconciliação
- [ ] Validar o fluxo completo de produção com a API Java
- [ ] Confirmar testes de ponta a ponta de webhook e emissão única de ingressos
- [ ] Adicionar documentação OpenAPI baseada nos contratos atuais
- [ ] Instrumentar observabilidade e alertas operacionais

## 🤝 Ecossistema Ticketfy

Este repositório implementa o **serviço de pagamentos**. A plataforma completa também utiliza a API principal Ticketfy, responsável pelos pedidos, eventos, usuários e ingressos.

## 👨‍💻 Desenvolvimento

**Ticketfy Payments** — microserviço Go para integração de pagamentos Pix da plataforma Ticketfy.

---

<div align="center">

**Construído com Go para tornar o fluxo de pagamentos da Ticketfy mais modular, rastreável e confiável.**

</div>
