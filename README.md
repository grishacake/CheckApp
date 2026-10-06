# App Grabber — backend

Скелет бэкенда для хакатона. Сервер запускается, но данные из магазинов ещё не получает: парсер будет написан на хакатоне.

## Запуск

Нужен Go 1.24+.

```bash
make run           # или: go run ./cmd/server
```

Проверка:

```bash
curl localhost:8080/health          # ok
curl -i localhost:8080/api/apps/com.uchi.app   # пока 501 — заглушка
```

Через Docker:

```bash
docker build -t app-grabber-backend .
docker run -p 8080:8080 app-grabber-backend
```

## API

| Метод | Путь | Что делает |
|-------|------|------------|
| GET | `/health` | Проверить, что сервер жив. |
| GET | `/api/apps/{package_name}` | Карточка приложения. **Пока заглушка, отвечает 501.** |

Договорённый формат ответа для `/api/apps/{package_name}` (структура `model.App`):

```json
{
  "app_id": "com.uchi.app",
  "store_url": "https://play.google.com/store/apps/details?id=com.uchi.app&hl=ru",
  "name": "Учи.ру",
  "developer": "ООО \"Учи.ру\"",
  "icon_url": "https://play-lh.googleusercontent.com/...",
  "category": "Образование",
  "rating": 4.2
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
- Поля `store` и `fetched_at`, хранение в БД
