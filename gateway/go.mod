module github.com/AndreyQuantum/golang-study-ms/gateway

go 1.25.14

require (
	github.com/AndreyQuantum/golang-study-ms/ledger v0.0.0
	github.com/go-chi/chi/v5 v5.5.0
)

require (
	github.com/stretchr/testify v1.12.1
	go.yaml.in/yaml/v3 v3.0.5 // indirect
)

replace github.com/AndreyQuantum/golang-study-ms/ledger => ../ledger
