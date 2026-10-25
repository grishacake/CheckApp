# PostgreSQL и бэкенд через Compose

Команды выполняются из корня репозитория CheckApp. Нужны запущенный Docker Engine, Docker Compose и OpenSSL для первоначального создания секрета. Go и Goose на машине для запуска через Compose не требуются.

## Первая настройка

```bash
cp .env.example .env
sed -i "s/^JWT_SECRET=.*/JWT_SECRET=$(openssl rand -hex 32)/" .env
```

Выполняется один раз после клонирования. Если `.env` уже есть, не перезаписывайте его. `.env.example` хранится в Git, `.env` с реальными настройками — нет. POSTGRES_* и адрес в DATABASE_URL должны соответствовать друг другу. Данные существующей БД при изменении пароля в `.env` автоматически не меняются.

## Запуск

```bash
docker compose up -d --build --wait
```

Порядок: `db` становится healthy → `migrate` применяет новые миграции Goose → запускается `backend`. Ошибка миграций блокирует запуск нового бэкенда. Миграции уже выполненных версий повторно не применяются. Существующий volume `postgres_data` сохраняется.

Контейнеры получают DATABASE_URL и JWT_SECRET из `.env`; ручные `export` для Compose не нужны. DATABASE_URL внутри контейнера указывает на `db:5432`. Секрет JWT создаётся один раз. Фронтенд пока запускается отдельно.

Dockerfile содержит цели `backend` и `migrations`. Goose закреплён на v3.29.0 и собирается с драйвером PostgreSQL. Для сборки образов используется Go 1.26, который требуется этой версии Goose; go.mod и версии зависимостей приложения не менялись.

## Проверка

```bash
docker compose ps -a
docker compose logs migrate backend
curl http://localhost:8080/health
```

Ожидается: `db` и `backend` — healthy, `migrate` — Exited (0), `/health` — `ok`. Swagger: http://localhost:8080/docs. Пользователи остаются в PostgreSQL после `docker compose restart backend`.

DBeaver: host `localhost`, port `5435`, database/user/password `checkapp` при стандартных настройках `.env.example`. Таблицы находятся в `public`.

## Остановка и обновление

```bash
docker compose down
docker compose up -d --build --wait
```

Данные сохраняются. Команда `docker compose down -v` удаляет volume с данными. При изменении Go-кода или миграций используйте `--build`, чтобы пересобрать образы. Уже применённые SQL-файлы не меняем — добавляем новые миграции.

## Ручной запуск Go — при необходимости

Остановите контейнер бэкенда, чтобы освободить порт 8080:

```bash
docker compose stop backend
docker compose up -d --wait db
export DATABASE_URL='postgres://checkapp:checkapp@localhost:5435/checkapp?sslmode=disable'
export JWT_SECRET="$(openssl rand -hex 32)"
go run ./cmd/server
```

Адрес с `localhost:5435` используется только при запуске Go на компьютере. `go run` сам не читает `.env` и не применяет миграции; сначала выполните полный запуск Compose. Ручная генерация JWT_SECRET создаёт другой секрет, поэтому ранее выданные токены могут потребовать повторного входа.

Для интеграционного теста укажите отдельную тестовую БД с применёнными миграциями:

```bash
TEST_DATABASE_URL='postgres://user:password@localhost:5435/checkapp_test?sslmode=disable' \
  go test -mod=readonly ./internal/postgres -run TestUsersIntegration -v
```

Без TEST_DATABASE_URL тест пропускается. Обычные тесты: `go test -mod=readonly ./...`.
