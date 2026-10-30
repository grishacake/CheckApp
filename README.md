# App Grabber — backend

## Запуск

Нужны Docker Engine, Docker Compose и OpenSSL для создания секрета при первой настройке.

Первый запуск после клонирования:

```bash
cp .env.example .env
sed -i "s/^JWT_SECRET=.*/JWT_SECRET=$(openssl rand -hex 32)/" .env
docker compose up -d --build --wait
```

Если `.env` уже заполнен, достаточно последней команды. Compose запускает PostgreSQL, применяет миграции Goose и поднимает бэкенд. Повторять `export` не нужно. Секрет в `.env` сохраняется между запусками; файл исключён из Git и сборки образа.

Проверка:

```bash
docker compose ps -a
curl http://localhost:8080/health
```

Ожидается: `db` и `backend` — healthy, `migrate` — Exited (0), `/health` — `ok`. Пользователи сохраняются в PostgreSQL. Остановка: `docker compose down` (данные остаются).

Настройки, DBeaver и ручной запуск Go: [локальная БД](docs/database.md). Фронтенд пока запускается отдельно.

## Доступ к Google Play из России

Из России `play.google.com` блокируют по имени сайта (SNI), поэтому сервер соединяется
с `www.google.com` и просит в заголовке `Host` страницу `play.google.com` — VPN не нужен.
Если этот обход перестанет работать, укажите прокси. Настройки — в `.env` (образец в `.env.example`):

| Переменная | Что задаёт |
|------------|------------|
| `GP_PROXY_URL` | Прокси для запросов к Google: `http://ЛОГИН:ПАРОЛЬ@IP:ПОРТ`. Пусто — без прокси. |
| `GP_FRONT_HOST` | Хост вместо `play.google.com`, по умолчанию `www.google.com`. Пустое значение — ходить напрямую. |

Compose передаёт `.env` в контейнер бэкенда; после изменения перезапустите: `docker compose up -d --wait backend`.

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
| GET | `/api/favorites?limit=20&offset=0` | Избранное пользователя; нужен Bearer-токен. |
| PUT | `/api/favorites/{store}/{packageName}` | Добавить сохранённую карточку в избранное. |
| DELETE | `/api/favorites/{store}/{packageName}` | Удалить карточку из избранного. |
| GET | `/api/history?limit=20&offset=0` | История просмотров текущего пользователя; нужен Bearer-токен. |
| GET | `/api/apps/{package_name}` | Свежая карточка из БД или Google Play; успешный GET записывается в историю. Переданный неверный JWT даёт 401. |
| GET | `/api/search?q=google chrome` | Поиск по словам: до 5 карточек в порядке выдачи Google Play. В `q` можно передать ссылку Google Play или RuStore — вернётся одна карточка. |

По package name свежая карточка возвращается из PostgreSQL без повторного парсинга. Срок актуальности — `APP_CACHE_TTL` (по умолчанию `24h`, `0` отключает чтение кеша). Отсутствующая или устаревшая карточка получается из Google Play и сохраняется без дублей. Поиск по словам пока идёт в Google Play, результаты сохраняются в БД. Ошибки чтения кеша и сохранения карточки для ссылки возвращают 500.

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
