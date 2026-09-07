# Projeto Korp

Serviço HTTP em Go atrás de um proxy reverso NGINX, em containers, com Prometheus e
Grafana. O ambiente inteiro sobe com um comando Ansible.

```
$ curl http://localhost:80/projeto-korp
{"nome":"Projeto Korp","horario":"2026-09-07T00:33:18Z"}
```

As decisões técnicas estão em **[DECISOES.md](DECISOES.md)**.

## Topologia

```
         host :80        host :9090      host :3000
             |                |               |
 ============|================|===============|=============  VM
             v                v               v
        +---------+      +------------+  +---------+
   +--->|  nginx  |      | prometheus |<-| grafana |
   |    +----+----+      +--+------+--+  +---------+
   |         | :8080        |      |
   |         v              |      | coleta :9115
   |  +--------------+      |      v
   |  | http-server- |<-----+  +----------+
   |  | projeto-korp |coleta   | blackbox |
   |  | :8080  :9101 | :9101   +----+-----+
   |  +--------------+              |
   |                                | sonda GET /projeto-korp
   +--------------------------------+
 ============================================================

 rede korp-net     http-server-projeto-korp . nginx . blackbox . prometheus
 rede monitoring   prometheus . grafana . blackbox
```

A aplicação não publica porta no host — só o NGINX fala com ela. O `/metrics` fica numa
porta administrativa separada (9101), que não é publicada nem alcançável pelo proxy. O
Grafana está apenas na rede `monitoring`, então enxerga o Prometheus e mais nada.

## Ambiente

Demonstrado numa VM Incus chamada `lab` com Debian 13. O Ansible roda da estação de
trabalho e conecta pelo plugin `community.general.incus`, entrando como root.

Nada nas roles depende do Incus: o inventário traz um bloco `[korp]` comentado com
conexão SSH para rodar contra qualquer host Debian ou Ubuntu.

## Como executar

O Ansible precisa estar instalado na estação de trabalho — veja o
[guia de instalação oficial](https://docs.ansible.com/projects/ansible/latest/installation_guide/intro_installation.html).
Com ele disponível, instale as collections que o projeto usa:

```bash
ansible-galaxy collection install -r ansible/requirements.yaml
```

**1. Aponte o inventário.** O `ansible/inventory/hosts.ini` vem apontado para a VM do
laboratório. Para outro alvo, comente o bloco `[incus]` e descomente o `[korp]`:

```ini
[korp]
korp-01 ansible_host=10.0.0.10

[korp:vars]
ansible_user=root
ansible_ssh_private_key_file=~/.ssh/id_ed25519
```

O playbook usa `hosts: all`, então nenhuma role muda.

**2. Suba o ambiente.**

```bash
cd ansible
ansible -m ping all
ansible-playbook site.yaml
```

O playbook instala o Docker, cria a rede, constrói a imagem, sobe os cinco containers,
configura proxy e monitoramento, e valida o serviço exibindo a resposta:

```
TASK [Show the service response] ***********************************************
ok: [lab] =>
    msg:
        endpoint: http://localhost:80/projeto-korp
        resposta:
            horario: '2026-09-07T00:33:18Z'
            nome: Projeto Korp
```

A segunda execução dá `changed=0`.

Cada camada roda isolada por tag: `docker`, `app`, `proxy`, `monitoring`, `stack`,
`validate`.

## Acesso

| Componente | Endereço | Credenciais |
|---|---|---|
| Aplicação | `http://<host>/projeto-korp` | — |
| Prometheus | `http://<host>:9090` | — |
| Grafana | `http://<host>:3000` | `admin` / `admin` |

O Grafana abre com o datasource e o dashboard já provisionados, na pasta
**Projeto Korp**. Prometheus e Grafana são publicados em `0.0.0.0` porque a VM é o
limite de isolamento do laboratório.

Sem tráfego o dashboard aparece vazio. Para popular, da raiz do repositório:

```bash
URL=http://<host> DURATION=600 NOISE=10 ./scripts/load.sh
```

`NOISE` é a porcentagem de requisições enviadas a paths inexistentes, para o painel de
códigos de status ter o que mostrar.

## Disponibilidade

O enunciado deixou a definição em aberto. Uso três camadas, porque cada uma enxerga uma
falha que as outras não enxergam:

| Métrica | Responde | Não vê |
|---|---|---|
| `up{job="http-server-projeto-korp"}` | o processo responde à coleta? | se a resposta está correta |
| `job:http_requests:success_ratio5m` | as requisições dão certo? | o caminho até a aplicação |
| `probe_success` | o usuário consegue usar? | — |

As duas primeiras enxergam o serviço a partir da coleta; a terceira é uma requisição real
feita de fora pelo blackbox exporter, atravessando o NGINX. Existe falha em que a primeira reporta
tudo certo e a terceira reporta quebrado, e ambas estão corretas — o
[DECISOES.md](DECISOES.md) traz o caso reproduzido.

## Dashboard

Organizado por RED — *rate*, *errors*, *duration* — com disponibilidade e error budget
no topo. SLO alvo de 99,9%.

Os alertas `ServiceDown`, `ProbeFailing` e `HighErrorRate` ficam em
`http://<host>:9090/alerts`. Não há Alertmanager, então eles são visíveis mas não
notificam.

## Estrutura

```
app/          serviço em Go, Dockerfile e testes
deploy/       compose.yaml e as configurações que os containers montam
ansible/      inventário, group_vars, site.yaml e as roles docker/app/proxy/monitoring
scripts/      gerador de carga
```

Os arquivos de `deploy/` são estáticos e o Ansible apenas os copia; só o `.env` é
template, a partir de `ansible/group_vars/all.yaml`.

## Versões

Verificadas nas fontes oficiais em 04/09/2026.

| Componente | Versão |
|---|---|
| Go | 1.27.1 |
| NGINX | 1.30.4-alpine |
| Prometheus | v3.14.0 |
| Grafana | 13.2.1 |
| Blackbox exporter | v0.28.0 |
| client_golang | v1.24.1 |
| Runtime da aplicação | `distroless/static-debian13:nonroot`, fixado por digest |
