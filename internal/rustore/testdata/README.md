# HTML-карточки RuStore

Полные HTML-страницы сохранены 8 ноября 2026 года и сжаты gzip, как существующие
фикстуры Google Play. Тесты читают локальные снимки и не обращаются в сеть.

| Файл | Источник | Проверяемый случай |
| --- | --- | --- |
| `ru.ozon.app.android.html.gz` | https://www.rustore.ru/catalog/app/ru.ozon.app.android | Приложение с оценками, разработчик через ссылку на Organization |
| `org.telegram.messenger.web.html.gz` | https://www.rustore.ru/catalog/app/org.telegram.messenger.web | Целый рейтинг в HTML (`4,0`), разработчик через `#dev` |
| `com.unicostudio.braintest.html.gz` | https://www.rustore.ru/catalog/app/com.unicostudio.braintest | Игра с вложенной категорией |
| `mobi.avva.lider.html.gz` | https://www.rustore.ru/catalog/app/mobi.avva.lider | Нет оценок, нет aggregateRating |
| `com.gs.doubler.yourname.html.gz` | https://www.rustore.ru/catalog/app/com.gs.doubler.yourname | Второй пример без оценок |
| `com.example.calculatorKSA1211.html.gz` | https://www.rustore.ru/catalog/app/com.example.calculatorKSA1211 | Отклонение package name с заглавными буквами |

Ожидаемые рейтинги соответствуют видимой HTML-микроразметке карточек: значения
в JSON-LD этих же страниц иногда округлены иначе. При обновлении снимков
проверьте все ожидаемые поля в `parser_test.go`.
