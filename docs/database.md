# Локальная БД

Команды выполняются из корня репозитория CheckApp. Нужны Docker Engine, Docker Compose и Go.

## Первый запуск

```bash
docker compose up -d --wait db
go install github.com/pressly/goose/v3/cmd/goose@v3.29.0
export DATABASE_URL='postgres://checkapp:checkapp@localhost:5435/checkapp?sslmode=disable'
"$(go env GOPATH)/bin/goose" -dir migrations postgres "$DATABASE_URL" up
```

Миграции создают таблицы и добавляют магазины: Google Play активен, RuStore — заглушка.

## Повторный запуск

Если БД остановлена:

```bash
docker compose up -d --wait db
```

Если БД уже работает, ничего запускать не нужно. Данные сохраняются в Docker volume. Повторно устанавливать Goose не требуется.

## Новые миграции

После добавления новых SQL-файлов:

```bash
export DATABASE_URL='postgres://checkapp:checkapp@localhost:5435/checkapp?sslmode=disable'
"$(go env GOPATH)/bin/goose" -dir migrations postgres "$DATABASE_URL" up
```

Goose применит только новые миграции. Уже применённые файлы не меняем.

## DBeaver

PostgreSQL: host `localhost`, port `5435`, database/user/password `checkapp`. Таблицы находятся в схеме `public`.

## Остановка

```bash
docker compose stop db
```

Данные сохраняются. Команда `docker compose down -v` удаляет volume вместе с данными.

## Зависимости бэкенда — второй этап

Добавлен `pgx/v5 v5.7.6`, требование проекта осталось Go 1.24.5.

После установки pgx сборка Swagger сообщала об отсутствующей контрольной сумме `golang.org/x/tools`. Она добавлена в `go.sum` командой:

```bash
go mod download golang.org/x/tools
```

Версии Swagger не менялись. Проверка `go build -mod=readonly ./...` прошла. Повторять исправление при каждом запуске не нужно.

## Запуск бэкенда с пулом БД

```bash
docker compose up -d --wait db
export DATABASE_URL='postgres://checkapp:checkapp@localhost:5435/checkapp?sslmode=disable'
export JWT_SECRET="$(openssl rand -hex 32)"
go run ./cmd/server
```

При старте сервер создаёт пул pgx и проверяет БД с тайм-аутом 5 секунд. При неверных настройках или недоступной БД запуск завершится с ошибкой. При `Ctrl+C` HTTP-серверу даётся до 10 секунд на завершение запросов, затем пул закрывается.

Пользователи пока остаются в памяти: PostgreSQL-хранилище — следующий шаг. В Compose пока только PostgreSQL; запуск бэкенда и Goose добавим позже.
