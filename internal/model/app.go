// Package model — формат данных, который бэкенд отдаёт фронтенду.
package model

// App — карточка приложения из магазина.
// Пока только 7 базовых полей, о которых договорились в чате.
type App struct {
	AppID     string  `json:"app_id"`
	StoreURL  string  `json:"store_url"`
	Name      string  `json:"name"`
	Developer string  `json:"developer"`
	IconURL   string  `json:"icon_url"`
	Category  string  `json:"category"`
	Rating    float64 `json:"rating"` // 0, если оценок нет. TODO: обсудить, может лучше null

	// TODO (хакатон): store, fetched_at — нужны для сравнения магазинов и истории.
}
