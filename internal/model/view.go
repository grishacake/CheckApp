package model

import "time"

type View struct {
	ID         int64     `json:"id"`
	StoreAppID int64     `json:"store_app_id"`
	Store      string    `json:"store"`
	App        App       `json:"app"`
	ViewedAt   time.Time `json:"viewed_at"`
}
