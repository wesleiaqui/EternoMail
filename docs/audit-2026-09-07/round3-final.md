# Rodada 3 final — 2026-09-07

## Escopo e preservação

Auditoria concluída sobre o workspace existente, preservando as Rodadas 1/2, a migração já implementada e os ajustes de release previamente presentes. Nenhum commit, reset ou descarte foi feito. Foram lidos CONTRIBUTING.md e docs/EXT_RULES.md; não foram encontrados AGENTS.md ou CLAUDE.md aplicáveis. A referência docs/EXTENSION_ARCHITECTURE.md não existe neste checkout.

Mapa revisado: app/Wails e lifecycle; account/folder/message/draft/settings; crypto/credentials/keyring/database; MIME/email/sync/IMAP/SMTP; OAuth/IPC; PGP/S/MIME/certificate; contatos/CardDAV/Google/Microsoft; extensões e calendário; senderlogo/notificações/atualizações/plataforma; frontend/viewers/links/caches; dependências e manifests Flatpak. As prioridades novas foram integração da migração com startup real, processos independentes, anexos hostis, ownership no bridge, lifecycle de contatos e caches. As correções anteriores foram mantidas e revalidadas, sem reescrever protocolos ou migrations SQL distribuídas.

## Achados

| Arquivo | Severidade | Problema | Evidência | Correção | Teste | Status |
|---|---|---|---|---|---|---|
| internal/pgp/store.go; internal/smime/store.go | 🟠 Alta | Default aceitava chave/certificado de outra conta | ID estrangeiro podia alterar flags e ponteiro da conta | Validar existência e proprietário dentro da transação, antes de alterar defaults | round3_test.go em ambos os pacotes | CORRIGIDO |
| app/search.go; app/settings.go; internal/sync/search.go | 🟠 Alta | Busca por pasta e recibos não verificavam a conta informada | Pasta/mensagem de outra conta atingia operação/cache | Verificar ownership antes de busca, cache ou envio | app/round3_account_scope_test.go; sync/messages_integrity_test.go | CORRIGIDO |
| internal/email/attachment.go; download.go | 🟠 Alta | Double decode corrompia anexos; leitores ilimitados e repetição após erro | Anexo literal SGVsbG8= virava Hello; leitor permanentemente falho não terminava | Usar Body já decodificado, limitar leituras e erros consecutivos | email/round3_test.go, inclusive SaveAttachment e leitura do arquivo | CORRIGIDO |
| internal/email/limits.go; attachment.go; download.go; internal/sync/parse.go | 🟠 Alta | MIME profundamente aninhado e TNEF malformado provocavam consumo excessivo/panic | 100 níveis; offsets inválidos; contagem MAPI 0xffffffff | Profundidade 64, limite 50 MiB já adotado no sync, validação estrutural e contenção do decoder legado | MIME/TNEF/leitor infinito em email/round3_test.go | CORRIGIDO |
| internal/credentials/store.go; migration.go | 🟠 Alta | Chave ausente com ciphertext persistido permitia criar chave incompatível | Banco legado sem device.key | Recusar criação e preservar banco para restauração da chave original | TestMissingKeyPreservesExistingCredentials | CORRIGIDO |
| internal/crypto/crypto.go; lock.go | 🟡 Média | Symlink de recovery/lock e retomada antes do rename com USER alterado | Symlink pendente; recovery válido com chave ainda v1 | Lstat, SameFile, arquivo regular; usar recovery validado na retomada | TestDanglingRecoverySymlinkDoesNotCreateKey; TestKeyLockRejectsSymlink; TestRecoveryBeforeKeyReplacement | CORRIGIDO |
| internal/crypto/keyfile_windows.go; keyfile_unix.go | 🟡 Média | Windows não oferece o fsync de diretório usado no Unix | Ordenação de persistência do rename | MoveFileEx com REPLACE_EXISTING e WRITE_THROUGH no Windows; rename e sync de diretório no Unix | Cross-compilação Windows/Darwin; testes de rename em Linux | CORRIGIDO |
| app/app.go | 🟡 Média | Preflight repetido substituía stores; falha deixava DB aberto; shutdown concorrente | Preflight exposto e múltiplas chamadas de fechamento | sync.Once, fechamento em falha e estado de saída atômico | round3_preflight_linux_test.go; round3_test.go | CORRIGIDO |
| internal/carddav/scheduler.go; sync.go; client.go; internal/contact/*_sync.go | 🟡 Média | Stop não aguardava todos os workers e cancelamento não chegava às requisições | Worker manual ativo durante Stop | Registrar workers antes do Wait e propagar contexto por chamada | carddav/round3_test.go; contact/round3_test.go | CORRIGIDO |
| internal/contact/microsoft_sync.go | 🟡 Média | Resposta 410 no full sync provocava recursão sem fim | Servidor sintético sempre responde 410 | Fazer fallback somente a partir de delta | Testes de contagem 1/2 requisições | CORRIGIDO |
| frontend/src/lib/stores/contactPhotos.svelte.ts; accounts.svelte.ts | 🟡 Média | Resposta atrasada repopulava cache invalidado | Resposta antiga entregue depois da nova | Geração e proteção do inflight; invalidar após remoção de conta | frontend/tests/contactPhotos.test.mjs | CORRIGIDO |
| extensions/calendar/backend/freebusy_aggregator.go; provider_local_freebusy.go | 🟡 Média | Cache global por email/dia misturava intervalo/store e preservava dados desativados | Mesmo dia com intervalos distintos e dois stores | Consultar estado atual sem cache global e ignorar fontes desativadas | calendar/backend/round3_test.go | CORRIGIDO |
| internal/extensions/auth/transport.go | 🟡 Média | Retry OAuth podia reenviar corpo consumido e repetir refresh já realizado | POST consumido no primeiro 401; token rotacionado | GetBody, releitura do token sob lock, descarte limitado do 401 | extensions/auth/round3_test.go | CORRIGIDO |
| go.mod; go.sum; frontend/package-lock.json; manifests Flatpak | 🟠 Alta | Dependências com advisories alcançáveis/altos | govulncheck e npm audit | Atualizações compatíveis e manifests sincronizados, sem npm audit fix | Suítes completas; auditorias; validação de fontes Flatpak | CORRIGIDO |
| internal/credentials/migration.go e crypto | 🟡 Média | Suspeita de janela COMMIT → troca final da chave | Inspeção da ordem e processos que terminam em três estágios | A chave v2 já é persistida antes da transação; recovery permanece até commit durável | TestMigrationAcrossProcessExit; TestCredentialMigrationInterruptedAfterCommit | JÁ ESTAVA CORRETO |
| frontend/package-lock.json | 🟡 Média | 34 entradas moderadas remanescentes, incluindo cadeia Tiptap e esbuild | npm audit final exit 1 | Registrar impacto e necessidade de migração major do editor; sem exploração da aplicação reproduzida | Auditoria somente leitura e inspeção dos pontos de entrada | DOCUMENTADO |

## Migração v1 → v2 e cobertura real

Inventário programático independente do schema: 20 colunas com “encrypted”, das quais **14 dependem deste Encryptor**. O teste compara esse inventário com a migração e mantém seis exclusões explícitas. Outro teste enumera os arquivos produtores de Encryptor; buscas em app/internal/extensions conferiram os consumidores e demais chamadas Encrypt/Decrypt, distinguindo criptografia de protocolo.

| Tabela | Colunas recifradas | Identificação da linha |
|---|---|---|
| accounts | encrypted_password, encrypted_smtp_password, encrypted_access_token, encrypted_refresh_token | id |
| smime_certificates | encrypted_private_key | id |
| pgp_keys | encrypted_private_key | id |
| extension_secrets | encrypted_value | extension + key |
| contact_sources | encrypted_password, encrypted_access_token, encrypted_refresh_token | id |
| oauth_tokens | encrypted_access_token, encrypted_refresh_token | account_id + client_config_id |
| user_oauth_clients | encrypted | config_id |
| oauth_custom_providers | encrypted | account_id |

Produtores centrais: internal/credentials/store.go, oauth.go, oauth_clientconfig.go, oauth_user_creds.go e oauth_custom_provider.go. PGP, S/MIME e extensões usam os métodos correspondentes do store. As duas tabelas de configuração OAuth são opcionais/lazy; ausência é aceita. NULL e string vazia são preservados, sem virarem ciphertext de valor vazio.

Exclusões: drafts.encrypted e drafts.pgp_encrypted são flags; drafts.encrypted_body e drafts.pgp_encrypted_body são blobs PKCS7/PGP; messages.smime_encrypted e messages.pgp_encrypted são flags. TestMigrationDoesNotReencryptProtocolBlobs verifica preservação. Certificados públicos, hashes, salts, IDs e tokens exclusivamente no keyring não são recifrados.

O upgrade realista cria os 14 valores v1 distintos (incluindo JSON sintético de configurações OAuth), fecha o banco, reabre pelo NewStore atual, verifica semântica, fecha/reabre novamente e compara os ciphertexts: a segunda inicialização não recifra. Uma credencial nova é gravada e validada com v2. Não são usadas credenciais reais.

### Ordem durável e estados de interrupção

1. Serializar NewStore com .credentials.lock; NewEncryptor usa também .device-key.lock.
2. Ler/validar v1 e persistir device.key.legacy com material de recuperação, antes de substituir a chave.
3. Gravar v2 em temporário exclusivo, sincronizar, fazer rename e garantir a persistência aplicável à plataforma.
4. Recifrar as 14 colunas em uma transação na conexão dedicada com synchronous=FULL. Ciphertext que autentica com v2 é mantido; os demais precisam autenticar com a chave antiga.
5. Depois do COMMIT, remover recovery e sincronizar diretório no Unix. Somente depois publicar o Store inicializado.

| Estado | Comportamento e evidência |
|---|---|
| A — morte antes da transação | v2 + recovery permitem recifrar o banco v1 no próximo processo; testado por saída de subprocesso |
| B — morte durante recifragem | SQLite desfaz transação não commitada; recovery permite repetir; testado fechando processo sem executar defers |
| C — rollback SQLite | Erro SQL/trigger ou ciphertext inválido aborta tudo; valores anteriores permanecem; reparar registro permite nova tentativa |
| D — COMMIT antes de atualização final de device.key | Essa ordem não ocorre: device.key já é v2 antes de iniciar SQL. A janela real é COMMIT → remoção de .legacy. Startup autentica v2, não recifra outra vez e limpa recovery; testado em processo separado |
| E — chave atualizada e DB ainda v1 | Estado esperado recuperável: .legacy foi persistido primeiro e só sai após FULL COMMIT |
| F — rename falha | Erro explícito, destino original preservado; teste de falha de escrita atômica |
| G — disco cheio | Erros de write/sync/SQL impedem sucesso e a ordem retém recovery; não houve injeção real de ENOSPC ou falha de hardware |
| H — temporário órfão | Não é fonte autoritativa; nomes exclusivos evitam colisão; teste de startup com temporário antigo |
| I — duas instâncias | Locks de SO serializam inicialização/migração. Teste com três processos independentes abre o mesmo banco legado e todos leem v2 corretamente |
| J — startup parcialmente concluído | v1/recovery ou v2/recovery são retomados; recovery inválido produz erro preservando ambos os arquivos; testes cobrem estados reconhecidos |

Os testes de interrupção usam os.Exit para não executar defers, não equivalem a desligar a energia ou simular um controlador de disco que ignora fsync. SQLite FULL e a ordem de persistência são a garantia estrutural, sob as garantias normais do filesystem.

Rollback não implica reverter device.key para v1: a chave v2 permanece acompanhada da chave de recuperação compatível com o banco revertido. Essa dupla é intencional. Não apagar .legacy manualmente durante recuperação. Chave ausente com ciphertext impede startup e requer restaurar a original, sem criar uma substituta silenciosa.

V1 tem 64 bytes; v2 tem 65 bytes versionados. Formato vazio, truncado, excessivo, versão desconhecida e recovery inconsistente falham preservando o material. Arquivos são 0600 e diretórios criados 0700; temporários exclusivos, verificação de symlink e identidade do lock reduzem substituições inesperadas. Não há promessa de proteção contra outro processo malicioso executando como o mesmo usuário e capaz de alterar todo o diretório privado. Primeiro upgrade de v1 sem recovery ainda depende do ambiente legado de derivação; falha explícita preserva o arquivo. Depois de recovery válido, mudança de USER não impede retomada.

### Concorrência

O teste com oito goroutines NewEncryptor verifica concorrência dentro do processo. TestConcurrentStoreProcesses cobre separadamente três processos de SO com NewStore. flock/LockFileEx são a proteção de migração; mutex local sozinho não seria suficiente. A detecção normal de instância única ocorre antes de Preflight, mas o socket Linux é uma conveniência cuja falha não deve ser confundida com exclusão criptográfica. As garantias valem para instâncias atuais que respeitam os locks; não se promete execução simultânea de um binário antigo já aberto que ignore o protocolo novo.

## Integração e áreas restantes

Preflight agora é idempotente; uma falha fecha o banco aberto e não publica a.db. Shutdown concorrente executa cleanup uma vez. Foram revisados cancelamento, espera dos schedulers, IMAP IDLE/pool, IPC e OAuth com as proteções anteriores preservadas e testes sob race. Chamadas CardDAV/Google/Microsoft iniciadas pelo scheduler recebem contexto cancelável. Bancos de extensões têm lifetime até saída do processo conforme EXT_RULES R11; não foi introduzida uma reescrita do lifecycle nem alegada quiescência completa de todos os timers de extensões.

Métodos Wails sensíveis foram agrupados por operações de conta/pasta/mensagem, anexos e filesystem, crypto/importação/exportação, URLs, OAuth/configuração e extensões. A lista completa das declarações expostas consta no apêndice. Não foram adicionadas assinaturas Wails e não foi necessário regenerar bindings. Acesso a arquivos escolhidos pelo usuário e drag/drop é capacidade intencional do aplicativo desktop; mensagem remota não recebe esse privilégio: iframe sem allow-same-origin, sanitização de HTML, validação de event.source e allowlist de esquemas de links permanecem. Testes de links cobrem escapes, atributos, links não aninhados, Unicode, HTTP/HTTPS/mailto e esquemas rejeitados. Não houve teste E2E de interação humana em WebKit.

Import/export de PGP/S/MIME, seleção de defaults, caminhos de anexos, SQL/FKs/transactions/NULL, conversões de modseq, erros descartados, nil/panic e timeouts foram revisados com testes anteriores preservados. Migrações SQL antigas não foram editadas. URLs de atualização são consultadas com timeout e resposta limitada; atualização não executa instalador remoto automaticamente. Sender logos possuem timeout e limite de resposta. Logging de migração não imprime chave, salt, plaintext ou ciphertext completo. Testes e logs novos usam dados sintéticos; arquivos de segredos reais não foram utilizados.

### Caches

| Cache/estado | Chave e isolamento/invalidação |
|---|---|
| Inline attachments | UUID global da mensagem; geração invalida requisições na troca/remoção de conta; cleanup impede resposta de view antiga |
| Conversation/PGP/S/MIME no viewer | Conta/pasta/thread + geração da view; respostas atrasadas descartadas |
| Body persistido | ID global da mensagem, associação da conta e FKs; ownership adicional nas entradas de busca |
| Contact photos | Agenda compartilhada por email; geração e inflight protegidos; remoção de conta invalida |
| Sender logos | Domínio público; compartilhamento intencional, não conteúdo privado de mensagem |
| Calendar free/busy | Cache global removido; consulta respeita intervalo e store atuais e fonte habilitada |
| Contatos/vCard e image allowlist | Estado compartilhado da agenda/configuração conforme produto; não tratado como armazenamento de corpo privado por conta |

## Validação

Go executado no container anclo-dev com GTK/WebKit, Go 1.26.7-X:nodwarf5 linux/amd64 e GOFLAGS=-tags=webkit2_41. Build usa safe.directory apenas por variáveis da invocação, sem alterar configuração Git persistente.

| Comando | Exit code / resultado final |
|---|---|
| GOFLAGS=-tags=webkit2_41 go test ./... -race -count=1 | 0; suíte completa, sem data race; app 16s, credentials 117s, settings 188s; pacotes sem testes explicitados no log |
| GOFLAGS=-tags=webkit2_41 go vet ./... | 0 |
| GOFLAGS=-tags=webkit2_41 go build ./... | 0, com GIT_CONFIG_COUNT=1, KEY_0=safe.directory e VALUE_0=caminho do checkout |
| npm run check | 0; 0 erros e 0 warnings |
| node --test tests/*.test.mjs (frontend) | 0; 5/5 testes |
| npm run build | 0; prebuild real executado; warnings de chunks grandes e imports dinâmicos/estáticos |
| npm test | Não executado: package.json não define script test. Foram executados os testes Node reais acima |
| govulncheck ./... (tool temporário e tag WebKit) | 0; zero vulnerabilidades alcançáveis, zero em pacotes importados; 4 em módulos requeridos sem chamadas afetadas |
| npm audit --json | 1; 34 moderate, 0 high, 0 critical, 0 low |
| GOOS=windows/darwin CGO_ENABLED=0 go test -c ./internal/crypto | 0 em ambas; somente cross-compilação, sem execução nessas plataformas |
| Validação dos sources Flatpak contra lockfile | PASS; 498 entradas de pacotes, 493 arquivos únicos com URL/integridade correspondentes |
| git diff --check | 0 |

Logs finais copiados junto deste relatório. A suíte com race é também a execução completa de go test ./..., não apenas uma seleção de pacotes. Não há necessidade de desativar race no ambiente final. Scripts lint/knip existem, mas não foram executados; não são substituídos ou declarados aprovados por check/build.

Tentativas anteriores: Go ausente no PATH do host, cache/GTK/sockets indisponíveis no ambiente anterior e VCS ownership no container foram contornados usando o container e a configuração por comando. Uma execução inicial foi interrompida manualmente durante migrations lentas de settings e não é usada como evidência de aprovação. Houve falhas intermediárias de fixture (ID de evento ausente e inserção SQL de mensagem com campos NULL) e uma referência de contexto em função standalone; foram corrigidas e a suíte completa final passou. Um govulncheck antigo/toolchain antigo foi substituído por ferramenta temporária compatível. Nenhuma dessas tentativas é ocultada como sucesso.

## Compatibilidade

- Upgrade v1 → v2: 14 valores distintos e tipos de credencial verificados após fechar/reabrir banco.
- Startup seguinte: ciphertext permanece idêntico; novo valor usa v2.
- Rollback: transação inteira revertida; recovery retém chave antiga; reparo e nova tentativa funcionam.
- Dados legados: NULL/vazios, tabelas OAuth opcionais e blobs de protocolo preservados; material inválido não é sobrescrito.
- Keyring: caminho de fallback cifrado e mocks testados; nenhum segredo real nem sessão real de keyring foi usado.
- Plataformas: execução Linux; crypto cross-compilado Windows/macOS. ACLs e crash real dessas plataformas não foram validados em runtime.

## Dependências e riscos restantes

Go: x/image v0.45.0, x/sys v0.47.0, x/text v0.41.0 e go-pkcs12 v0.7.2; fontes Flatpak atualizadas. Dependências npm selecionadas foram atualizadas dentro dos ranges existentes por vulnerabilidades concretas, sem upgrade major e sem npm audit fix. O grande diff de node-sources.json é saída do gerador contra o lock atual, conferida programaticamente; package.json mantém as versões/release previamente editadas.

1. npm ainda reporta 34 entradas moderadas, em grande parte propagadas pela cadeia Tiptap, além de esbuild transitivo. Não são 34 vulnerabilidades independentes. O advisory Tiptap envolve mergeAttributes/prototype pollution e exige migração major para versão corrigida; o fluxo inspecionado utiliza HTML/schema, sem exploração por entrada externa reproduzida nesta aplicação. Isso não elimina a dependência vulnerável. esbuild remanescente está na cadeia de ferramentas; não corresponde ao servidor de produção do aplicativo. Referência: https://github.com/ueberdosis/tiptap/security/advisories/GHSA-cp6q-959q-f8rh (o advisory usa severidade High, enquanto o npm final classifica a cadeia como moderate).
2. Ausência de E2E GUI com troca rápida real de contas/mensagens e interação com iframe; há testes de geração/cache e revisão dos guards.
3. Sem injeção de disco cheio, falha de fsync, corte físico de energia ou runtime Windows/macOS. Processos interrompidos são evidência de recuperação lógica, não certificação de hardware. WRITE_THROUGH segue a API documentada: https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-movefileexw.
4. Keyring desktop real e autenticação com provedores externos não foram exercitados; testes usam mocks/servidores locais.
5. Duas versões distintas executando simultaneamente, com a antiga ignorando locks, não são cenário garantido. Encerrar a versão antiga antes do upgrade continua necessário.
6. Remover cache free/busy aumenta consultas, mantendo o timeout existente. TNEF continua usando decoder legado, agora limitado/validado e com proteção contra panic; não houve fuzzing prolongado.
7. Flatpak completo e instaladores não foram produzidos. O manifest de release já continha tag v0.3.6 com commit 4c33a9821c86778e33003aa2d9e18e325b3c1e93; esse pin prévio foi preservado e não representa as alterações ainda não commitadas desta rodada. Antes de publicar, o processo de release precisa apontar ao commit que realmente inclua as correções. As fontes de dependências foram sincronizadas e a compilação Go/frontend passou. Resultado de govulncheck refere-se à toolchain atual; não certifica binários construídos com a antiga Go 1.25.3.

Não foi identificado risco conhecido restante de perda de credenciais na migração suportada, corrupção transacional ou bypass crítico nos caminhos corrigidos/testados. Isso não representa prova formal de ausência de defeitos em todo o aplicativo.

## Alterações realizadas

Arquivos de produção trabalhados nesta rodada: app/app.go, app/search.go, app/settings.go; internal/pgp/store.go; internal/smime/store.go; internal/email/attachment.go, download.go, limits.go; internal/sync/parse.go, search.go; internal/credentials/store.go, migration.go; internal/crypto/crypto.go, lock.go, keyfile_unix.go, keyfile_windows.go; internal/carddav/client.go, sync.go, scheduler.go; internal/contact/google_sync.go, microsoft_sync.go; internal/extensions/auth/transport.go; extensions/calendar/backend/freebusy_aggregator.go, provider_local_freebusy.go; frontend/src/lib/stores/contactPhotos.svelte.ts, accounts.svelte.ts; go.mod/go.sum; frontend/package-lock.json; build/flatpak/flathub/go.mod.yml, modules.txt, node-sources.json.

Testes novos/estendidos: app/round3_test.go, round3_preflight_linux_test.go, round3_account_scope_test.go; internal/credentials/migration_test.go e round3_test.go; internal/crypto/migration_test.go; internal/email/round3_test.go; internal/pgp/round3_test.go; internal/smime/round3_test.go; internal/carddav/round3_test.go; internal/contact/round3_test.go; internal/extensions/auth/round3_test.go; internal/sync/messages_integrity_test.go; extensions/calendar/backend/round3_test.go; frontend/tests/contactPhotos.test.mjs e testes de links existentes ampliados. As demais alterações abaixo incluem material prévio preservado, não atribuído integralmente à Rodada 3.

Revisão final: diff de código, testes novos e arquivos gerados conferidos; nenhum novo executável, dump, .tmp ou marcador temporário de debug identificado no conjunto alterado. Artefatos de toolchain/testes ficaram em /tmp; frontend/dist é saída ignorada de build. Nenhum segredo real foi acrescentado. Gofmt aplicado aos arquivos Go trabalhados.

## Apêndice — superfície Wails

Declarações atuais dos bindings, listadas para permitir revisão/reprodução do inventário (não é alegação de teste individual de cada método):

### App.d.ts — 347 métodos

AcceptCertificate, AddAccount, AddContact, AddContactSource, AddImageAllowlist, AddMicrosoftSharedMailbox, AddPGPKeyServer, AddSpellcheckCustomWord
Archive, BroadcastThemeChange, Calendar_AddCalDAVSource, Calendar_AddGoogleSource, Calendar_AddLocalCalendar, Calendar_AddLocalSource, Calendar_AddMicrosoftSource, Calendar_CreateEvent
Calendar_DeleteCalendar, Calendar_DeleteEvent, Calendar_DeleteSource, Calendar_DismissAlarm, Calendar_ForceSyncSource, Calendar_GetEvent, Calendar_GrantCalendarAccess, Calendar_ListCalendars
Calendar_ListEventsInRange, Calendar_ListGoogleCalendarsForAccount, Calendar_ListMicrosoftCalendarsForAccount, Calendar_ListSources, Calendar_LogFrontend, Calendar_OpenURL, Calendar_QueryFreeBusy, Calendar_RenameSource
Calendar_ReprobeCalDAVOrganizerIdentities, Calendar_SearchContacts, Calendar_SetCalendarColor, Calendar_SetCalendarVisible, Calendar_SetDisplayTimezone, Calendar_SetOrganizerIdentity, Calendar_SetSyncInterval, Calendar_SyncAllSources
Calendar_SyncSource, Calendar_UpdateEvent, Calendar_UpdateMyAttendeeStatus, CanUndo, CancelAccountSync, CancelAllSyncs, CancelContactSourceOAuthFlow, CancelFolderSync
CancelOAuthFlow, CheckForUpdates, CheckRecipientCerts, CheckRecipientPGPKeys, ClearOAuthCreds, CloseWindow, CompleteContactSourceOAuthSetup, CompleteCustomOAuthAccountSetup
CompleteOAuthAccountSetup, Contacts_CreateContact, Contacts_DeleteLocalContact, Contacts_EnableWriteAccess, Contacts_GetContactDetail, Contacts_LinkAccountSource, Contacts_ListAddressbooks, Contacts_ListContactsForBrowse
Contacts_ListSources, Contacts_ResizeContactPhoto, Contacts_SyncAllSources, Contacts_SyncSource, Contacts_UpdateContact, CopyToFolder, CreateIdentity, DeleteContact
DeleteContactSource, DeleteDraft, DeleteIdentity, DeleteLocalMessages, DeletePGPKey, DeletePGPSenderKey, DeletePermanently, DeleteSMIMECertificate
DeleteSenderCert, DiscoverCardDAVAddressbooks, DiscoverCardDAVAddressbooksOAuth, DiscoverOAuthProvider, DownloadAttachment, DownloadEncryptedAttachment, EmptyTrash, EmptyUnifiedTrash
FetchMessageBody, FetchServerMessage, FindLocalMessageIDs, ForceSyncContactSource, ForceSyncFolder, GetAccentBarUnread, GetAccount, GetAccountFoldersForMapping
GetAccountProfilePhotos, GetAccounts, GetAllAccountIdentities, GetAlwaysLoadImages, GetAlwaysShowMessageCheckbox, GetAppInfo, GetAttachment, GetAttachments
GetAutoCheckUpdates, GetAutoDetectedFolders, GetAutostart, GetComposerFormat, GetComposerMode, GetConfiguredOAuthProviders, GetConnectedComposers, GetContact
GetContactPhotos, GetContactSource, GetContactSourceErrors, GetContactSourceStats, GetContactSources, GetContext, GetConversation, GetConversationCount
GetConversations, GetCustomOAuthAccounts, GetDarkComposerBody, GetDarkMailContent, GetDraft, GetDraftForEdit, GetFTSIndexStatus, GetFTSIndexStatusAll
GetFolderTree, GetFolders, GetIMAPConnectionForUndo, GetIPCAddress, GetIdentities, GetIdentity, GetImageAllowlist, GetInlineAttachments
GetLanguage, GetLastSeenVersion, GetLastUpdateCheck, GetLinkedAccountsForContactSync, GetMailtoMode, GetMarkAsReadDelay, GetMessage, GetMessageCount
GetMessageListDensity, GetMessageListSortOrder, GetMessageSource, GetMessages, GetMicrosoftSharedMailboxes, GetNativeTitleBar, GetOAuthBuildStatus, GetOAuthCredsChoices
GetOAuthCredsStatus, GetOAuthStatus, GetOAuthWarningDisabled, GetPGPEncryptPolicy, GetPGPKeyForEmail, GetPGPKeyServers, GetPGPSignPolicy, GetPendingMailto
GetReadReceiptResponsePolicy, GetRunBackground, GetSMIMECertificateForEmail, GetSMIMEEncryptPolicy, GetSMIMESignPolicy, GetSearchCount, GetSearchCountUnifiedFolder, GetSearchCountUnifiedInbox
GetSenderLogos, GetShowMessageListCircles, GetShowMessageListProfilePics, GetShowTitleBar, GetShowViewerCircles, GetSkippedUpdateVersion, GetSourceAddressbooks, GetSpecialFolder
GetSpellcheckCustomWords, GetSpellcheckEnabled, GetSpellcheckLanguages, GetStartHidden, GetStartHiddenActive, GetSystemTheme, GetTermsAccepted, GetThemeMode
GetTrustedCertificates, GetUIState, GetUndoDescription, GetUnifiedFolderConversations, GetUnifiedFolderCount, GetUnifiedInboxConversations, GetUnifiedInboxCount, GetUnifiedInboxUnreadCount
GetWindowDecorationStatus, HasPGPKey, HasSMIMECertificate, IMAPSearchFolder, IMAPSearchUnifiedInbox, IgnoreReadReceipt, ImportPGPKeyFromPath, ImportRecipientCert
ImportRecipientPGPKey, ImportSMIMECertificateFromPath, ImportSMIMECertificateFromPathBER, InitiateShutdown, IsExtensionEnabled, IsFTSIndexComplete, IsFTSIndexing, IsFlatpak
IsImageAllowed, IsOAuthConfigured, IsReady, LinkAccountContactSource, ListAccountSetupHooksForProvider, ListAuthContextsForProvider, ListContacts, ListDrafts
ListEnabledExtensions, ListExtensionRailTabs, ListExtensions, ListPGPKeys, ListPGPSenderKeys, ListSMIMECertificates, ListSenderCerts, LogFrontend
LookupHKP, LookupPGPKey, LookupWKD, MarkAllFolderMessagesAsRead, MarkAllFolderMessagesAsUnread, MarkAsNotSpam, MarkAsRead, MarkAsSpam
MarkAsUnread, MoveLocalMessages, MoveMessagesToFolder, MoveToFolder, NotifyStartupComplete, OpenAttachment, OpenComposerWindow, OpenEncryptedAttachment
OpenFile, OpenFolder, OpenURL, PickAttachmentFiles, PickPGPKeyFile, PickRecipientCertFile, PickRecipientPGPKeyFile, PickSMIMECertificateFile
Preflight, PrepareReply, ProcessPGPMessage, ProcessSMIMEMessage, QuitApp, ReadFileAsAttachment, ReauthorizeAccount, RebuildFTSIndex
RefreshWindowConstraints, RemoveAccount, RemoveFromInbox, RemoveImageAllowlist, RemovePGPKeyServer, RemoveSpellcheckCustomWord, RemoveTrustedCertificate, ReorderAccounts
SaveAllAttachments, SaveAllEncryptedAttachments, SaveAttachmentAs, SaveDraft, SaveEncryptedAttachmentAs, SavePendingOAuthTokens, SaveUIState, SearchContacts
SearchConversations, SearchUnifiedFolder, SearchUnifiedInbox, SendMessage, SendReadReceipt, SetAccentBarUnread, SetAccountEnabled, SetAddressbookEnabled
SetAlwaysLoadImages, SetAlwaysShowMessageCheckbox, SetAutoCheckUpdates, SetAutostart, SetComposerFormat, SetComposerMode, SetContactSourceWritable, SetDarkComposerBody
SetDarkMailContent, SetDefaultIdentity, SetDefaultPGPKey, SetDefaultSMIMECertificate, SetExtensionEnabled, SetLanguage, SetLastSeenVersion, SetLastUpdateCheck
SetMailtoMode, SetMarkAsReadDelay, SetMessageListDensity, SetMessageListSortOrder, SetNativeTitleBar, SetOAuthCreds, SetOAuthCredsChoice, SetOAuthWarningDisabled
SetPGPEncryptPolicy, SetPGPSignPolicy, SetReadReceiptResponsePolicy, SetRunBackground, SetSMIMEEncryptPolicy, SetSMIMESignPolicy, SetShowMessageListCircles, SetShowMessageListProfilePics
SetShowTitleBar, SetShowViewerCircles, SetSkippedUpdateVersion, SetSpellcheckEnabled, SetSpellcheckLanguages, SetStartHidden, SetTermsAccepted, SetThemeMode
ShowWindow, Star, StartContactsOnlyOAuthFlow, StartCustomOAuthFlow, StartOAuthFlow, SubscribeAllFolders, SubscribeFolder, SyncAccountComplete
SyncAllComplete, SyncAllContactSources, SyncContactSource, SyncFolder, SyncFolders, SyncPendingDrafts, SyncUnifiedFolder, TestCardDAVConnection
TestConnection, TestOAuthConnection, TestSMTPConnection, Trash, Undo, Unstar, UnsubscribeFolder, UpdateAccount
UpdateContactSource, UpdateIdentity, UpdateLocalFlags

### ComposerApp.d.ts — 44 métodos

BeforeClose, CheckRecipientCerts, CheckRecipientPGPKeys, CloseWindow, DeleteDraft, GetAccount, GetAllAccountIdentities, GetComposeMode
GetDarkComposerBody, GetDraft, GetIdentities, GetNativeTitleBar, GetOriginalMessage, GetPGPEncryptPolicy, GetPGPKeyForEmail, GetPGPSignPolicy
GetSMIMECertificateForEmail, GetSMIMEEncryptPolicy, GetSMIMESignPolicy, GetShowTitleBar, GetSystemTheme, GetThemeMode, GetWindowDecorationStatus, HasPGPKey
HasSMIMECertificate, ImportRecipientCert, ImportRecipientPGPKey, IsFlatpak, LogFrontend, LookupHKP, LookupPGPKey, LookupWKD
NotifyStartupComplete, PickAttachmentFiles, PickRecipientCertFile, PickRecipientPGPKeyFile, PrepareReply, ReadFileAsAttachment, RefreshWindowConstraints, SaveDraft
SearchContacts, SendMessage, Shutdown, Startup

## Apêndice — git status --short

Snapshot após criação do relatório; inclui alterações anteriores preservadas.

```text
 M CHANGELOG.md
 M README.md
 M VERSION
 M app/account.go
 M app/app.go
 M app/attachment.go
 M app/oauth.go
 M app/search.go
 M app/settings.go
 M build/flatpak/flathub/go.mod.yml
 M build/flatpak/flathub/io.github.wesleiaqui.eternomail.yml
 M build/flatpak/flathub/modules.txt
 M build/flatpak/flathub/node-sources.json
 M build/flatpak/io.github.wesleiaqui.eternomail.metainfo.xml
 M extensions/calendar/backend/freebusy_aggregator.go
 M extensions/calendar/backend/provider_local_freebusy.go
 M frontend/package-lock.json
 M frontend/package.json
 M frontend/package.json.md5
 M frontend/src/App.svelte
 M frontend/src/lib/components/WhatsNewDialog.svelte
 M frontend/src/lib/components/viewer/AttachmentList.svelte
 M frontend/src/lib/components/viewer/ConversationViewer.svelte
 M frontend/src/lib/components/viewer/EmailBody.svelte
 M frontend/src/lib/i18n/locales/en.json
 M frontend/src/lib/i18n/locales/pt-BR.json
 M frontend/src/lib/stores/accounts.svelte.ts
 M frontend/src/lib/stores/contactPhotos.svelte.ts
 M frontend/src/lib/stores/inlineAttachmentCache.ts
 M go.mod
 M go.sum
 M internal/carddav/client.go
 M internal/carddav/scheduler.go
 M internal/carddav/sync.go
 M internal/contact/google_sync.go
 M internal/contact/microsoft_sync.go
 M internal/credentials/oauth.go
 M internal/credentials/oauth_clientconfig.go
 M internal/credentials/oauth_custom_provider.go
 M internal/credentials/oauth_slot_alias.go
 M internal/credentials/oauth_user_creds.go
 M internal/credentials/store.go
 M internal/crypto/crypto.go
 M internal/crypto/crypto_test.go
 M internal/database/database.go
 M internal/email/attachment.go
 M internal/email/download.go
 M internal/extensions/auth/transport.go
 M internal/folder/store.go
 M internal/imap/client.go
 M internal/imap/idle.go
 M internal/imap/pool.go
 M internal/ipc/client.go
 M internal/ipc/server.go
 M internal/ipc/server_unix.go
 M internal/ipc/token.go
 M internal/keyring/keyring.go
 M internal/oauth2/flow.go
 M internal/oauth2/server.go
 M internal/oauth2/server_test.go
 M internal/pgp/store.go
 M internal/smime/store.go
 M internal/smime/verifier.go
 M internal/smtp/client.go
 M internal/smtp/client_test.go
 M internal/sync/charset.go
 M internal/sync/header_recovery.go
 M internal/sync/helpers.go
 M internal/sync/messages.go
 M internal/sync/messages_integrity_test.go
 M internal/sync/parse.go
 M internal/sync/scheduler.go
 M internal/sync/search.go
 M wails.json
?? app/round3_account_scope_test.go
?? app/round3_preflight_linux_test.go
?? app/round3_test.go
?? docs/audit-2026-09-07/
?? extensions/calendar/backend/round3_test.go
?? frontend/src/lib/utils/emailLinks.ts
?? frontend/tests/
?? internal/carddav/round3_test.go
?? internal/contact/round3_test.go
?? internal/credentials/audit_test.go
?? internal/credentials/migration.go
?? internal/credentials/migration_test.go
?? internal/credentials/round3_test.go
?? internal/crypto/audit_test.go
?? internal/crypto/keyfile_unix.go
?? internal/crypto/keyfile_windows.go
?? internal/crypto/lock.go
?? internal/crypto/lock_unix.go
?? internal/crypto/lock_windows.go
?? internal/crypto/migration_test.go
?? internal/database/audit_test.go
?? internal/email/download_test.go
?? internal/email/limits.go
?? internal/email/round3_test.go
?? internal/extensions/auth/round3_test.go
?? internal/folder/audit_test.go
?? internal/imap/audit_test.go
?? internal/ipc/audit_test.go
?? internal/ipc/audit_unix_test.go
?? internal/ipc/reader.go
?? internal/keyring/audit_test.go
?? internal/oauth2/flow_test.go
?? internal/oauth2/token_clear.go
?? internal/oauth2/token_clear_test.go
?? internal/pgp/round3_test.go
?? internal/smime/round3_test.go
?? internal/smime/verifier_test.go
?? internal/sync/audit_test.go
```

## Release readiness

A suíte completa com race, vet, build e validações frontend passou. Recuperação e concorrência entre processos estão cobertas; as limitações acima permanecem explícitas, especialmente dependências npm e plataformas sem runtime.

READY WITH DOCUMENTED LIMITATIONS
