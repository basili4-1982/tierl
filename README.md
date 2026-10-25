# tierl

Tier list service in Go. Work in progress.

## Почта

Письма подтверждения email и сброса пароля отправляются через SMTP
(`TIERL_SMTP_*`). В `docker compose up` уже поднят Mailpit: он принимает всю
почту на `mailpit:1025`, а веб-интерфейс доступен на <http://localhost:8025>.
Если `TIERL_SMTP_HOST` не задан, письма не отправляются, а в лог пишутся только
получатель и тема — токены и ссылки в лог не попадают. Ссылки в письмах
строятся от `TIERL_APP_BASE_URL`.
