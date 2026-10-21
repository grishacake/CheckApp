package model

type App struct {
	PackageName     string  `json:"package_name"`
	StoreURL  			string  `json:"store_url"`
	Name      			string  `json:"name"`
	Developer 			string  `json:"developer"`
	IconURL   			string  `json:"icon_url"`
	Category  			string  `json:"category"`
	Rating    			float64 `json:"rating"`
	AgeRating 			string  `json:"age_rating"`
}
