package entity

type Budget struct {
	Category string  `json:"category"`
	Limit    float64 `json:"limit"`
}
