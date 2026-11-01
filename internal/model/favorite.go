package model

import "time"

type Favorite struct {
	StoreAppID int64     `json:"store_app_id"`
	Store      string    `json:"store"`
	App        App       `json:"app"`
	CreatedAt  time.Time `json:"created_at"`
}
