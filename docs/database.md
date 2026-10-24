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

В Compose пока только PostgreSQL. Подключение pgx и перенос запуска бэкенда и Goose в Compose — следующие этапы.
