module github.com/AndreyQuantum/golang-study-ms/gateway

go 1.25.14

require (
	github.com/AndreyQuantum/golang-study-ms/ledger v0.0.0
	github.com/go-chi/chi/v5 v5.5.0
)

replace github.com/AndreyQuantum/golang-study-ms/ledger => ../ledger
