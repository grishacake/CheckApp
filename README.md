# App Grabber — backend

## Запуск

Нужен Go 1.24.5+ и запущенный Docker для PostgreSQL.

```bash
docker compose up -d --wait db
export DATABASE_URL='postgres://checkapp:checkapp@localhost:5435/checkapp?sslmode=disable'
export JWT_SECRET="$(openssl rand -hex 32)"
make run           # или: go run ./cmd/server
```

Без `JWT_SECRET`, `DATABASE_URL` или доступной БД сервер не запустится. Соединение проверяется при старте с тайм-аутом 5 секунд. Пользователи пока хранятся в памяти и исчезают после перезапуска. При `Ctrl+C` сервер завершает запросы и закрывает пул БД.

Проверка:

```bash
curl localhost:8080/health          # ok
curl -i localhost:8080/api/apps/com.uchi.app
```

Миграции, DBeaver и повторный запуск: [локальная БД](docs/database.md). В Docker Compose пока запускается только PostgreSQL; бэкенд запускается отдельно командой выше.

## Swagger / OpenAPI

После запуска откройте http://localhost:8080/docs - интерактивная документация и примеры восьми полей.
Спецификация: http://localhost:8080/openapi.json или файл [docs/openapi.json](docs/openapi.json).
Пример карточки для моков: [docs/examples/app.json](docs/examples/app.json).

## API

Авторизация и пошаговая проверка в Swagger: [JWT, настройки и middleware](docs/auth.md).

| Метод | Путь | Что делает |
|-------|------|------------|
| GET | `/health` | Проверить, что сервер жив. |
| POST | `/api/auth/register` | Регистрация: email, обязательный ник, пароль. |
| POST | `/api/auth/login` | Вход и получение JWT. |
| GET | `/api/auth/me` | Профиль по Bearer-токену. |
| GET | `/api/apps/{package_name}` | Карточка приложения из Google Play: 400 неверный формат, 404 не найдено, 502 Google не ответил. |
| GET | `/api/search?q=google chrome` | Поиск по словам: до 5 карточек в порядке выдачи Google Play, `{"query": ..., "results": [...]}`. |

Договорённый формат ответа для `/api/apps/{package_name}` (структура `model.App`):

```json
{
  "package_name": "com.uchi.app",
  "store_url": "https://play.google.com/store/apps/details?id=com.uchi.app&hl=ru",
  "name": "Учи.ру",
  "developer": "ООО \"Учи.ру\"",
  "icon_url": "https://play-lh.googleusercontent.com/...",
  "category": "Образование",
  "rating": 4.2,
  "age_rating": "3+"
}
```

## Структура

```
cmd/server/main.go        HTTP-сервер: роуты, JSON-ответы, CORS
internal/model/app.go     Структура App — формат ответа фронтенду
Makefile                  Короткие команды: run, test, build
Dockerfile                Сборка образа
```

## На хакатон

- Получение данных из Google Play (клиент + парсер) в `internal/googleplay/`
- Сохранить настоящие страницы 10–20 приложений в `testdata/` и написать тесты на них
- Ошибки: неверный ID → 400, приложение не найдено → 404
- Возрастной рейтинг `age_rating` — восьмое поле карточки.
- Поиск по названию, БД, история и сравнение магазинов не входят в текущий хакатон.
