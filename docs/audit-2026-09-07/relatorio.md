# Auditoria e correção — 7 de setembro de 2026

Auditoria das oito áreas solicitadas, com correções no código e testes de regressão. Alterações que já estavam no workspace foram preservadas. Nenhum esquema de credenciais foi alterado e nenhuma credencial real foi regravada durante a auditoria.

## Achados

“NÃO REPRODUZÍVEL” significa que a condição descrita não está presente nos caminhos examinados, não uma garantia de ausência de outros defeitos.

| Arquivo | Severidade | Descrição | Status |
|---|---|---|---|
| `internal/imap/idle.go` | Alta | `Stop` liberava o estado `running` antes da saída da goroutine, permitindo substituir seus canais. Agora mantém o estado até a saída; chamadas concorrentes de parada não fecham o canal duas vezes. Inicialização pelo manager passou a ocorrer antes de publicar a conclusão de StartAccount. | CORRIGIDO |
| `internal/imap/idle.go` | Alta | `doneCh` já era fechado no `defer`, inclusive após esgotar tentativas. Adicionado teste desse caminho. | NÃO REPRODUZÍVEL |
| `internal/imap/idle.go` | Média | Backoff inicial podia exceder o teto; duplicação podia sofrer overflow. Aplicado teto desde a primeira espera e duplicação sem overflow. Falha ao coletar FETCH agora gera Warn. | CORRIGIDO |
| `internal/imap/pool.go` | Alta | Disponibilidade era publicada antes de adquirir o lock do pool; uma conexão podia ser reservada enquanto era entregue a um waiter. Transferência agora é atômica sob o lock do pool; cancelamento também recupera entregas que já venceram a fila. | CORRIGIDO |
| `internal/imap/pool.go` | Alta | Double-acquire descrito já é evitado por `isHealthyLocked`. O pool atual só faz uma repetição específica de 15 segundos; não possui `MaxReconnectBackoff`. O backoff exponencial está no IDLE. | NÃO REPRODUZÍVEL |
| `internal/imap/client.go` | Média | Tipo de segurança desconhecido deixava cliente nil e causava panic. Agora retorna erro. | CORRIGIDO |
| `internal/sync/folderlock.go` | Alta | Os caminhos examinados adquirem uma pasta por operação, sem aquisição A→B/B→A. Não foi criado método de locks múltiplos sem consumidor. | NÃO REPRODUZÍVEL |
| `internal/sync/fetch.go`, `search.go` | Alta | Leitura do corpo já usa `io.LimitReader` e `maxMessageSize = 50 * 1024 * 1024`. | NÃO REPRODUZÍVEL |
| `internal/sync/messages.go`, `header_recovery.go` | Alta | Leitura de headers IMAP era ilimitada. Aplicado limite também nesses caminhos. Erros nas consultas de localização Gmail agora são registrados e preservam a mensagem local. | CORRIGIDO |
| `internal/sync/parse.go`, `helpers.go`, `header_recovery.go` | Média | Subject/From e destinatários eram persistidos sem limite/remoção de NUL. Sanitização aplicada ao envelope normal e à recuperação; limite de 8192 bytes preserva UTF-8 e JSON válido. Leitura TNEF com erro agora gera Warn. | CORRIGIDO |
| `internal/folder/store.go`, `internal/sync/condstore.go` | Alta | MODSEQ negativo no SQLite era convertido em enorme `uint64`. Todas as leituras agora aceitam somente valores positivos; zero entra no full sync já implementado no CONDSTORE. | CORRIGIDO |
| `internal/sync/charset.go` | Média | Fallback de decode para conteúdo original já existe. Removido preview de HTML dos logs, que podia conter conteúdo privado e URLs com tokens. | CORRIGIDO |
| `internal/sync/scheduler.go` | Alta | Goroutines por conta não eram aguardadas no Stop. Trabalho agora é registrado antes da parada, recebe contexto capturado, respeita cancelamento no semáforo e impede reinício durante a drenagem. | CORRIGIDO |
| `internal/database/database.go` | Alta | Caminho podia alterar opções do DSN. Rejeita `?`, `&`, `#`, aplica `filepath.Clean` e codifica URI, inclusive `%` literal. Rollback registra erros, exceto `sql.ErrTxDone`. | CORRIGIDO |
| `internal/database/migrations.go` | Média | Migrações são SQL estático e o registro de versão usa placeholder. Não há SQL concatenado com dados variáveis para converter. | NÃO REPRODUZÍVEL |
| `internal/crypto/crypto.go` | Alta | Derivação dependia de USER/USERNAME. Agora usa `aerion:<hostname>:<uid>`, conforme solicitado. Consequência de compatibilidade comentada no código. | CORRIGIDO |
| `internal/crypto/crypto.go` | Alta | Arquivo de chave inválido ou erro de leitura levava à regeneração. Tamanho inválido e erros diferentes de arquivo ausente agora retornam erro sem sobrescrever o arquivo. | CORRIGIDO |
| `internal/crypto/crypto.go` | Alta | Formato legado não possui campo de versão/migração; troca da derivação altera a chave de dados antigos. Ver decisão de compatibilidade abaixo. | DOCUMENTADO |
| `internal/credentials/store.go`, `oauth.go`, `oauth_user_creds.go`, `oauth_custom_provider.go`, `oauth_slot_alias.go` | Média | Limpeza no SQLite/keyring descartava erros. Adicionados logs de falha; exclusão conjunta agrega erros retornados. | CORRIGIDO |
| `internal/credentials/oauth_clientconfig.go` | Média | SQL era concatenado com identificador de coluna. Agora seleciona consultas completas estáticas com valores parametrizados e rejeita identificadores desconhecidos. | CORRIGIDO |
| `internal/credentials/oauth.go`, `app/account.go` | Alta | Exclusão apagava metadados antes de enumerar slots OAuth de extensões, deixando tokens no keyring. Slots são enumerados e removidos antes do cascade da conta. | CORRIGIDO |
| `internal/credentials/oauth.go`, `internal/oauth2/token_clear.go`, `app/oauth.go`, `app/account.go` | Média | Adicionados métodos Clear; tokens pendentes são limpos ao consumir/cancelar fluxo e ao remover a conta correspondente. | CORRIGIDO |
| `internal/credentials/oauth*.go`, `store.go`, `internal/database/migrations.go` | Alta | Tokens/senhas já usam keyring ou Encryptor; SQLite contém ciphertext. `oauth_user_creds.go` guarda configuração de cliente OAuth cifrada, não senha IMAP/SMTP. Não foram inseridos comentários alegando texto claro onde ele não existe. | NÃO REPRODUZÍVEL |
| `internal/keyring/keyring.go` | Média | Erros expunham mensagens internas do backend e exclusões eram ignoradas. Retorno descritivo recomenda sessão desktop/keyring desbloqueado, logs não incluem payload e exclusões agregam erros. Serviço já é `aerion`. | CORRIGIDO |
| `internal/ipc/reader.go`, `server.go`, `client.go` | Alta | JSON sem limite e leitores recriados após auth perdiam bytes já recebidos. Limite por mensagem de 10 MB mantém read-ahead, inclusive mensagens pipelined. | CORRIGIDO |
| `internal/ipc/server.go`, `token.go` | Alta | Estado autenticado tinha acesso concorrente desprotegido; TokenManager sem inicialização aceitava token vazio. Corrigidos. Primeira mensagem diferente de auth já era rejeitada, agora coberta por teste. | CORRIGIDO |
| `internal/ipc/server.go`, `client.go`, `server_unix.go` | Alta | Cancelamento podia deixar Accept bloqueado; corrida entre Stop e registro de cliente; socket não era removido na saída de Start. Corrigidos, com chmod 0600 antes de aceitar conexões e limpeza em defer. | CORRIGIDO |
| `frontend/src/lib/components/viewer/EmailBody.svelte`, `utils/emailLinks.ts` | Alta | Duas substituições de links podiam inserir links dentro de atributos já gerados. Agora escapa texto/atributos e processa matches uma única vez; permite somente HTTP(S)/mailto, impede navegação por clique auxiliar e remove permissão de popups do iframe. | CORRIGIDO |
| `frontend/src/lib/components/viewer/EmailBody.svelte` | Alta | HTML normal, recuperado e decifrado passa por bluemonday no backend (`fetch.go`, `ParseDecryptedBody`). Único `{@html}` recebe texto escapado pelo linkificador. | NÃO REPRODUZÍVEL |
| `frontend/src/lib/components/viewer/AttachmentList.svelte` | Média | Resposta atrasada podia substituir anexos da mensagem atual. Guarda de identidade adicionada e estado anterior limpo ao carregar. Nomes já usam interpolação escapada. | CORRIGIDO |
| `frontend/src/lib/components/viewer/ConversationViewer.svelte` | Alta | Respostas antigas e resultados decifrados podiam reaparecer após trocar contexto. Geração de visualização invalida resultados anteriores; limpa conteúdo, referências e resultados de S/MIME/PGP. Não há `{@html}` próprio nesse componente. | CORRIGIDO |
| `frontend/src/lib/stores/inlineAttachmentCache.ts`, `accounts.svelte.ts`, `frontend/src/App.svelte` | Média | Cache de anexos sobrevivia à troca/remoção de conta; respostas em voo podiam repovoá-lo. Limpeza com contador de geração invalida respostas anteriores. | CORRIGIDO |

## Testes e evidências

Novos testes nos arquivos `audit_test.go` dos pacotes imap, sync, database, crypto, credentials, keyring, ipc e folder, além de `ipc/audit_unix_test.go`, `oauth2/token_clear_test.go` e `frontend/tests/email-audit.test.mjs`.

Cobrem: saída e reinício do IDLE, teto/overflow do backoff, configuração inválida, publicação de conexão no pool, headers/UTF-8/JSON, charset desconhecido, cancelamento do scheduler, DSN e `%` literal, independência de USER/USERNAME, preservação de chave corrompida, erro de limpeza, identificador SQL, remoção de slots no keyring, Clear, JSON excessivo/pipelined, pré-auth, token vazio, socket/cancelamento, links/escape/protocolos e invalidação do cache. Testes existentes de CONDSTORE verificam full sync quando a baseline é zero.

Comandos principais:

```sh
go build ./...
go test ./internal/imap/... ./internal/sync/... ./internal/database/... ./internal/crypto/... ./internal/credentials/... ./internal/keyring/... ./internal/ipc/... -race -count=1 -v
go test ./internal/folder/... ./internal/sync/... -race -count=1
go test ./internal/oauth2/... ./app/... -race -count=1
cd frontend
npm run check
node --test tests/email-audit.test.mjs
```

Execução Linux no contêiner de desenvolvimento existente `anclo-dev`, Go 1.26.7, Node 22.23.1, GTK 3.24.52 e WebKitGTK 2.52.5. Para o build, configuração temporária de `git safe.directory` via variáveis de ambiente permitiu leitura dos metadados VCS do workspace compartilhado; nenhuma configuração global foi alterada.

O shell inicial não possuía Go no PATH nem bibliotecas GTK. Go do arquivo já existente no repositório foi extraído em `/tmp` para as primeiras verificações. O build completo foi validado no contêiner com as bibliotecas presentes.

Resultados completos em arquivos adjacentes: `go-tests.txt`, `frontend-check.txt`, `frontend-tests.txt`, `related-tests.txt`, `folder-final.txt`. `build.txt` fica vazio quando `go build ./...` termina sem diagnósticos. O aviso separado do Browserslist informa apenas base caniuse desatualizada; Svelte terminou com 0 erros e 0 avisos.

## Resultado final

- `go build ./...`: exit 0, sem diagnósticos.
- Sete pacotes solicitados: todos PASS com `-race -count=1 -v`.
- Pacotes relacionados `internal/folder`, `internal/oauth2` e `app`: PASS com `-race`.
- `npm run check`: 0 erros, 0 avisos do Svelte.
- Testes de frontend: 3 aprovados, 0 falhas.
- `git diff --check`: sem erros.

## Decisões do mantenedor e limites

1. **Compatibilidade da chave:** a fórmula solicitada muda a chave derivada de um `device.key` antigo, embora o arquivo continue intacto. Ciphertexts existentes no fallback podem deixar de abrir e exigir migração explícita ou reentrada das credenciais. Antes de distribuir, decidir a política de migração/versionamento para usuários existentes. Não foi criada regeneração automática nem migração silenciosa. Contas com credenciais no keyring não dependem dessa derivação para seus valores primários.
2. **Identidade do dispositivo:** hostname e UID continuam compondo a chave, conforme solicitado; mudança de máquina/usuário continua afetando compatibilidade. O formato legado também guarda bytes da chave no arquivo; não foi redesenhado nesta tarefa.
3. **Headers extensos:** Subject/From são truncados e listas de destinatários são reduzidas preservando JSON. Conteúdo excedente de headers não é mantido nesses campos de exibição.
4. **Clear:** remove referências a strings sensíveis; não garante apagar cópias físicas da memória gerenciada por Go. Exclusões externas que falhem ficam registradas; isso não equivale a confirmação de remoção pelo keyring.
5. **Plataformas e UI:** testes IPC de socket são Linux; named pipes e keyrings reais de cada desktop não foram exercitados. Frontend foi verificado pelo Svelte e testes Node das funções de segurança/cache, sem teste visual ou E2E de interação no WebView.
6. **TLS:** `InsecureSkipVerify` com verificação customizada de certificados não foi reportado, conforme instrução explícita.
