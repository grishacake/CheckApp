# JWT

Подготовлены `auth.TokenManager`, middleware `httpapi.WithAuth` и `httpapi.AuthHandler`. Для подключения к основному серверу нужна реализация `auth.UserRepository`. Текущий `cmd/server/main.go` пока не регистрирует маршруты авторизации и не читает настройки JWT.

## HTTP-контракт

| Метод | Путь | Запрос / ответ |
| --- | --- | --- |
| POST | `/api/auth/register` | JSON: `email`, обязательный `nickname`, `password`. Ответ 201: пользователь. |
| POST | `/api/auth/login` | JSON: `email`, `password`. Ответ 200: `access_token`, `token_type: "Bearer"`, `user`. |
| GET | `/api/auth/me` | Bearer-токен. Ответ 200: пользователь из БД. |

Пользователь содержит `id`, `email`, `nickname`, `created_at`. Хеш пароля не возвращается. После регистрации нужен отдельный вход. Все ответы авторизации имеют `Cache-Control: no-store`.

Для POST нужен `Content-Type: application/json`. Максимальный размер тела — 8 КиБ; неизвестные поля и несколько JSON-объектов отклоняются. Ошибки имеют вид `{"error":"сообщение"}`: 400 — неверные данные, 401 — неверные данные входа или токен, 409 — занятый email, 413 — слишком большое тело, 415 — неверный Content-Type, 500 — внутренняя ошибка, 504 — тайм-аут. Полный контракт доступен в Swagger.

После реализации хранилища создайте сервис через `auth.NewService(repository)`, менеджер токенов через `auth.NewTokenManagerFromEnv()` с проверкой ошибки и обработчик через `httpapi.NewAuthHandler(service, tokens, 5*time.Second)`. Вызов `handler.RegisterRoutes(mux)` подключает три маршрута, включая middleware для `/me`. При подключении нужно также разрешить `POST` и `Authorization` в CORS сервера.

## Настройки JWT

`NewTokenManagerFromEnv()` читает:

| Переменная | Значение |
| --- | --- |
| `JWT_SECRET` | Обязательный случайный секрет, минимум 32 байта. Одинаковый для экземпляров одного бэкенда. |
| `JWT_TTL` | Время жизни access-токена, по умолчанию `15m`. Формат Go duration, например `30m` или `1h`; минимум `1s`. |

Подготовка окружения в Git Bash:

```bash
export JWT_SECRET="$(openssl rand -hex 32)"
export JWT_TTL=15m
```

Сохраните секрет в настройках окружения сервера, чтобы он не менялся при каждом запуске. Смена секрета делает ранее выпущенные токены недействительными. Не добавляйте секрет в Git.

`Issue(userID)` выпускает токен HS256. Поля: `sub` — ID пользователя, `iat` — время выпуска, `exp` — окончание действия, `iss` — `app-grabber`, `aud` — `app-grabber-api`. Email, ник и хеш пароля в токен не входят.

`Verify(token)` проверяет подпись, алгоритм, срок действия, издателя, получателя и наличие ID пользователя. При ошибке возвращает `auth.ErrInvalidToken`.

Защищённый обработчик оборачивается в `httpapi.WithAuth(tokens, handler)`. Клиент передаёт `Authorization: Bearer <token>`. Middleware возвращает JSON с кодом 401 при отсутствии или недействительности токена; при успехе ID доступен через `httpapi.UserIDFromContext(r.Context())`.

Проверка токена не обращается к БД. Refresh-токены и отзыв отдельных токенов пока не реализованы: после истечения срока требуется повторный вход.
