# auth-service (Go)

Este é o serviço de autenticação do projeto ToggleMaster. Ele é responsável por criar e validar chaves de API.

## 📦 Pré-requisitos (Local)

* [Go](https://go.dev/doc/install) (versão 1.25.13 ou superior)
* [PostgreSQL](https://www.postgresql.org/download/) (rodando localmente ou em um contêiner Docker)

## 🚀 Rodando Localmente

1.  **Clone o repositório** e entre na pasta `auth-service`.

2.  **Prepare o Banco de Dados:**
    * Crie um banco de dados no seu PostgreSQL (ex: `auth_db`).
    * Execute o script `db/init.sql` para criar a tabela `api_keys`:
        ```bash
        psql -U seu_usuario -d auth_db -f db/init.sql
        ```

3.  **Configure as Variáveis de Ambiente:**
    Crie um arquivo chamado `.env` na raiz desta pasta (`auth-service/`) com o seguinte conteúdo:
    ```.env
    # String de conexão do seu banco de dados PostgreSQL
    DATABASE_URL="postgres://SEU_USUARIO:SUA_SENHA@localhost:5432/auth_db"
    
    # Porta que o serviço irá rodar
    PORT="8001"
    
    # Chave mestra para criar novas chaves de API
    MASTER_KEY="admin-secreto-123"
    ```

4.  **Instale as Dependências:**
    ```bash
    go mod tidy
    ```

5.  **Inicie o Serviço:**
    ```bash
    go run .
    ```
    O servidor estará rodando em `http://localhost:8001`.

## ✅ Qualidade e testes

```bash
gofmt -w .
go vet ./...
go test -race -cover ./...
go build -o bin/auth-service .
docker build -t togglemaster-auth-service .
```

O workflow `.github/workflows/ci.yml` atende ao fluxo DevSecOps da Fase 3:

1. build e testes unitários com detector de corrida;
2. `gofmt` e `golangci-lint`;
3. SAST com `gosec` e SCA com Trivy;
4. build e scan da imagem Docker, bloqueando vulnerabilidades críticas;
5. em push na `main`, login e push para o ECR com tag imutável `sha-<commit>`;
6. atualização automática do overlay de homologação no repositório GitOps.

O workflow não cria repositórios via AWS CLI. O ECR
`togglemaster-homolog/auth-service` deve existir previamente e é gerenciado pelo
Terraform no repositório `togglemaster-infrastructure`.

### Configuração do GitHub Actions

Cadastre os seguintes secrets no repositório:

- `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` e `AWS_SESSION_TOKEN`: credenciais
  temporárias do AWS Academy;
- `GITOPS_TOKEN`: token com permissão de escrita em
  `devjean96/togglemaster-gitops`.

Credenciais do AWS Academy expiram e devem ser atualizadas antes da execução. Em
conta pessoal, prefira trocar as credenciais estáticas por OIDC com uma role de
escopo mínimo. Pull Requests não recebem credenciais AWS e nunca publicam imagens.

## 🧪 Testando os Endpoints

Você pode usar `curl` ou Postman.

**1. Verifique a Saúde (Health Check):**
```bash
curl http://localhost:8001/health
```

Saída esperada: `{"status":"ok"}`

**2. Crie uma nova Chave de API (requer a MASTER_KEY):**

```bash
curl -X POST http://localhost:8001/admin/keys \
-H "Content-Type: application/json" \
-H "Authorization: Bearer admin-secreto-123" \
-d '{"name": "meu-primeiro-servico"}'
``` 

Saída esperada (A SUA CHAVE SERÁ DIFERENTE):

```json
{
  "name": "meu-primeiro-servico",
  "key": "tm_key_a1b2c3d4...",
  "message": "Guarde esta chave com segurança! Você não poderá vê-la novamente."
}
```

**3. Valide a Chave que você acabou de criar: (Substitua tm_key_... pela chave exata que você recebeu no passo anterior)**

```bash
curl http://localhost:8001/validate \
-H "Authorization: Bearer tm_key_a1b2c3d4..."
```

Saída esperada: `{"message":"Chave válida"}`

**4. Teste uma Chave Inválida:**

```bash
curl http://localhost:8001/validate \
-H "Authorization: Bearer chave-errada-123"
```

Saída esperada: `Chave de API inválida ou inativa`
