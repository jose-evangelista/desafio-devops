# Projeto Korp — desafio DevOps

Serviço HTTP em Go atrás de um proxy reverso NGINX, em containers, com
monitoramento via Prometheus e Grafana, provisionado por um único comando
Ansible.

> Documentação em construção. O README completo — topologia, execução passo a
> passo e as decisões técnicas — é escrito na Fase 5. As decisões detalhadas
> ficam em `DECISOES.md`.

## Estado atual

| Fase | Escopo | Situação |
|------|--------|----------|
| 0 | Esqueleto do repositório e role `docker` do Ansible | concluída |
| 1 | Serviço Go, métricas e testes | pendente |
| 2 | Container da aplicação, compose e proxy NGINX | pendente |
| 3 | Prometheus, blackbox exporter e Grafana provisionado | pendente |
| 4 | Roles `app`, `proxy` e `monitoring` | pendente |
| 5 | Documentação | pendente |

## Ambiente de demonstração

Uma VM Incus chamada `lab`, rodando Debian 13 (trixie). O Ansible executa a
partir da estação de trabalho e conecta na VM pelo plugin de conexão
`community.general.incus`, entrando direto como root.

O inventário traz um bloco `[korp]` comentado com conexão SSH: nada nas roles
depende do Incus, então o mesmo playbook roda contra qualquer host Debian ou
Ubuntu trocando o bloco ativo em `ansible/inventory/hosts.ini`.

## Pré-requisitos na estação de trabalho

```bash
pipx install ansible-core                              # 2.21.3
ansible-galaxy collection install -r ansible/requirements.yml
```

## Execução

```bash
cd ansible
ansible-playbook site.yml
```

Para provisionar apenas o Docker:

```bash
ansible-playbook site.yml --tags docker
```
