module github.com/dirac-lee/domkit/example/order

go 1.27.0

replace github.com/dirac-lee/domkit => ../..

require (
	github.com/DATA-DOG/go-sqlmock v1.5.2
	github.com/dirac-lee/domkit v0.0.0-00010101000000-000000000000
	github.com/go-sql-driver/mysql v1.8.1
	github.com/redis/go-redis/v9 v9.22.0
	gorm.io/driver/mysql v1.6.0
	gorm.io/gorm v1.31.2
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/sys v0.30.0 // indirect
	golang.org/x/text v0.20.0 // indirect
)
