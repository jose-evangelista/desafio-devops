# Decisões técnicas

## Parte 1 — Serviço e arquitetura

### Serviço HTTP

Usei `net/http` da biblioteca padrão. O `ServeMux` do Go 1.22+ já casa método e path
(`mux.Handle("GET /projeto-korp", ...)`), que era a única coisa que justificaria um
roteador externo.

Além do mínimo pedido:

- timeouts explícitos no `http.Server` — os defaults do Go são zerados, ou seja, sem
  limite — e graceful shutdown em SIGTERM
- log estruturado em JSON com `log/slog`
- configuração por variável de ambiente, falhando na inicialização se algum valor for
  inválido
- `/healthz` e `/readyz`, deliberadamente não instrumentados: o healthcheck do Docker
  bate a cada 30s e dominaria a métrica de volume de requisições

Os testes cobrem status, content-type, o campo `nome`, e o `horario` parseável como
RFC3339 em UTC. Um deles garante que o objeto tem exatamente dois campos, porque o
enunciado define a estrutura da resposta.

### Dockerfile

Multi-stage. Build com `CGO_ENABLED=0 -trimpath -ldflags="-s -w"`, runtime em
`distroless/static-debian13:nonroot` fixado por digest, já que o Distroless só publica
canais móveis e não tem tag de versão. Considerei `scratch`, mas o distroless traz os CA
certificates e os metadados de pacote que os scanners de vulnerabilidade leem.

O `HEALTHCHECK` chama `/app -healthcheck`, uma flag do próprio binário — distroless não
tem shell nem curl. No compose o container roda com `read_only`, `cap_drop: ALL` e
`no-new-privileges`.

### Instalação do Docker

Escrevi a role do zero: repositório oficial com o codename lido dos facts, chave GPG,
pacotes e `daemon.json` com rotação de log. Em produção eu usaria a `geerlingguy.docker`.

O `python3-docker` vem do apt e não do pip, porque o Debian 13 aplica a PEP 668 e o pip
recusa a instalação.

### Rede

Separei em duas redes bridge para que o Grafana não tenha rota até a aplicação: ele consulta o
Prometheus e nada mais precisa alcançar.

Declarei `korp-net` como `external` e a crio pelo Ansible porque o enunciado pede a
criação da rede como passo do playbook; se o compose a criasse, o requisito ficaria
implícito.

### Compose

Healthcheck em todos os serviços, com `depends_on: service_healthy`. O NGINX só sobe
depois que a aplicação passa no healthcheck, senão ele subiria primeiro e falharia ao
resolver o upstream.

### Proxy reverso

Foi o ponto mais delicado. O NGINX resolve o nome do upstream uma vez, no boot, e guarda
o IP. Quando o container da aplicação é recriado com endereço novo, ele continua tentando
o IP antigo e passa a servir **502 com a aplicação saudável**.

Reproduzi com dois NGINX lado a lado, forçando a troca de IP:

```
app mudou de 172.18.0.4 -> 172.18.0.5

  proxy_pass literal:      502 Bad Gateway
  resolver + variável:     {"nome":"Projeto Korp", ...}
```

A correção é `resolver 127.0.0.11 valid=10s` (o DNS do Docker) com `proxy_pass`
apontando para uma variável, o que adia a resolução para o momento da requisição. Duas
armadilhas vêm junto: com variável a URI não é repassada sozinha, daí o `$request_uri`;
e barra final no `proxy_pass` reescreve o path.

**Catch-all em vez de bloqueio.** Considerei restringir o proxy a
`location = /projeto-korp` e devolver 404 no resto. Fiquei com o catch-all por dois
motivos: o enunciado manda encaminhar requisições da porta 80 para a 8080 sem qualificar
quais, e requisição barrada no NGINX não chega à aplicação, logo não entra na métrica de
volume. Com o catch-all, path errado aparece como 404 no dashboard sob um label fixo. O
`/metrics` continua inacessível porque está em outra porta, não por causa do `location`.

Isso teve um efeito colateral: registrar `/` fez `POST /projeto-korp` responder 404 em
vez de 405. Registrei a rota também sem método, com um handler que devolve 405 e o
header `Allow`.

## Parte 2 — Monitoramento

### Por que client_golang e não OpenTelemetry

O desafio pede métricas no formato do Prometheus, e o client_golang gera esse formato
direto. O OpenTelemetry chegaria no mesmo lugar passando por uma camada de tradução, que
existe para quando se quer trocar de backend depois ou juntar métricas com traces e logs.
Nenhum dos dois é o caso aqui, então escolhi a biblioteca que resolve o problema sem
intermediário.

A instrumentação está isolada no pacote `internal/metrics` — é o único lugar do código que
conhece o Prometheus. Se um dia entrar tracing distribuído, troca-se esse pacote e o resto
da aplicação não muda.

O desacoplamento é verificável: `go list -deps ./internal/httpapi` não retorna nada do
Prometheus. O contrato entre os pacotes é `func(http.Handler) http.Handler`.

### O que é exposto

`http_requests_total{method,code,path}`, `http_request_duration_seconds`,
`http_requests_in_flight` e `korp_build_info{version,commit}`, num registry próprio
servido na porta 9101.

O label `path` vem de uma constante de rota, nunca da URL. Se viesse da URL, requisições
com query string variável criariam uma série nova a cada chamada e o Prometheus cairia
por consumo de memória.

Separar a porta administrativa da porta pública deixa a proteção do `/metrics`
arquitetural: o NGINX só fala com a 8080, então não existe regra de bloqueio que alguém
possa remover sem perceber.

### Disponibilidade em três camadas

Usei três métricas em vez de uma porque nenhuma delas cobre sozinha o que "disponível"
significa — o README lista as três e o ponto cego de cada uma.

A terceira, `probe_success`, vem do blackbox exporter e é a única que testa o serviço de
fora. Ela não se contenta com o status 200: valida o corpo da resposta com uma regex
procurando `"nome": "Projeto Korp"`, então uma aplicação que passe a responder JSON
errado reprova na sonda.

É ela que enxerga o cenário do 502 reproduzido na Parte 1, em que `up` continua
reportando que está tudo bem — e está certo, porque o processo está mesmo de pé.

### Prometheus

Uma recording rule para o SLI, para a expressão viver num lugar só em vez de duplicada
entre painel e alerta. 404 conta como sucesso, seguindo a convenção de que 4xx é erro do
cliente e 5xx do serviço.

Três alertas, todos com cláusula `for` para que uma coleta ruim isolada não dispare nada:

| Alerta | Condição |
|---|---|
| `ServiceDown` | `up == 0` por 1 min |
| `ProbeFailing` | `probe_success == 0` por 1 min |
| `HighErrorRate` | 5xx acima de 1% por 5 min |

Testei parando a aplicação: `ServiceDown` e `ProbeFailing` foram a `firing` e voltaram a
`inactive` depois; `HighErrorRate` ficou inativo o tempo todo, que é o correto — sem
aplicação não há requisição, logo não há 5xx.

Não incluí Alertmanager porque notificação real traz roteamento, agrupamento e
silenciamento, e nada disso seria exercitado numa demonstração de uma VM só.

### Grafana

Provisionado por arquivo: `datasources.yaml`, `dashboards.yaml` e o JSON do dashboard. O
datasource tem `uid: prometheus` fixo, porque o dashboard o referencia por esse uid — vale
notar que exportar o dashboard pela interface com a opção "export for sharing externally"
troca isso por `${DS_PROMETHEUS}` e quebra o provisionamento.

Os alertas ficam no Prometheus e não no Grafana, pelo mesmo motivo: regra em arquivo
versionado passa por revisão e volta pelo playbook.

Um detalhe que só apareceu testando: o painel de taxa de erro ficava vazio justamente
quando o serviço estava saudável, porque `sum(rate(...{code=~"5.."}))` sobre conjunto
vazio devolve vetor vazio. Corrigido com `or vector(0)`.

## Parte 3 — Ansible

`site.yaml` fino chamando quatro roles com tags: `docker`, `app`, `proxy` e `monitoring`.
O `docker compose up` fica em `post_tasks` e não numa role, porque sobe os cinco serviços
de uma vez e não pertence a nenhuma delas.

Deixei os arquivos de `deploy/` estáticos, com só o `.env` como template, para o compose
continuar legível e para dar para subir o stack com `docker compose up` direto, sem
Ansible, durante o desenvolvimento. Transformar tudo em `.j2` custaria as duas coisas.

### Idempotência

A segunda execução dá `changed=0`. Três coisas costumam quebrar isso numa role de Docker:

- `apt` com `update_cache` reporta `changed` sempre, quando a task não instala pacote
  nenhum. Aqui o cache só é atualizado quando o repositório muda de fato.
- `curl | gpg --dearmor` é `shell`, logo `changed` eterno. Uso `get_url` com a chave
  `.asc`, que o APT aceita direto no `Signed-By`.
- `daemon.json` como template poderia variar a ordem das chaves entre execuções e
  reiniciar o Docker toda vez. É arquivo estático, comparado por checksum.

Não há `command` nem `shell` na role de Docker. O build da imagem usa `rebuild: never`,
então quem dispara reconstrução é bumpar `korp_app_version` — mudar o código sem bumpar a
versão não reconstrói, o que é a disciplina de tag imutável funcionando.

### Reload do NGINX

O handler valida com `nginx -t` dentro do container antes de recarregar com
`nginx -s reload`. Testei com uma configuração quebrada: o play falhou na validação e o
NGINX continuou servindo normalmente, com o mesmo PID. Recarregar em vez de recriar o
container também evita derrubar conexões em andamento.

Os handlers são guardados por `docker_container_info`, porque na primeira execução a
configuração chega antes de o container existir.

### Validação final

`ansible.builtin.uri` contra `http://localhost/projeto-korp` executando no alvo, com
`retries`/`until`, seguido de `assert` e `debug`. O assert verifica `nome`, a presença de
`horario` e que o objeto tem exatamente dois campos.

## O que eu faria diferente em produção

- **Alertmanager**, para as regras notificarem de fato.
- **Mais de uma réplica da aplicação** com o NGINX balanceando. Hoje ela é ponto único de
  falha, o que não sustenta um SLO de 99,9%.
- **Renovate ou Dependabot** para o digest da imagem base e as versões fixadas.
- **`read_only` no NGINX**, com tmpfs em `/var/cache/nginx` e `/var/run`.
- **Armazenamento remoto para o Prometheus**, em vez de 15 dias em volume local.
