package entity

type CategoryTotal struct {
	Category string  `json:"category"`
	Total    float64 `json:"total"`
}

type ReportSummary struct {
	From       string          `json:"from"`
	To         string          `json:"to"`
	Total      float64         `json:"total"`
	Categories []CategoryTotal `json:"categories"`
}
