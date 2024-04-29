package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"

	_ "github.com/lib/pq"
)

const (
	MetricTable = "request.metric"
)

var pool *sql.DB

func main() {
	var err error
	http.HandleFunc("/", UpdateAndView)
	http.HandleFunc("/hello", getHello)

	conf := DatabaseConfigBanking{
		Host:     os.Getenv("DATABASE_HOST"),
		Port:     os.Getenv("DATABASE_PORT"),
		Database: os.Getenv("DATABASE_NAME"),
		User:     os.Getenv("DATABASE_USER"),
		Password: os.Getenv("DATABASE_PASSWORD"),
	}

	fmt.Println(os.Getenv("DATABASE_PASSWORD"))

	// Opening a driver typically will not attempt to connect to the database.
	dsn := conf.DSN() // `postgres://postgres:mysecretpassword@localhost:5432/mos?sslmode=disable`
	pool, err = sql.Open("postgres", dsn)
	if err != nil {
		// This will not be a connection error, but a DSN parse error or
		// another initialization error.
		log.Fatal("unable to use data source name", err)
	}
	defer pool.Close()

	pool.SetConnMaxLifetime(0)
	pool.SetMaxIdleConns(3)
	pool.SetMaxOpenConns(3)

	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	appSignal := make(chan os.Signal, 3)
	signal.Notify(appSignal, os.Interrupt)

	go func() {
		<-appSignal
		stop()
	}()

	if err = pool.PingContext(ctx); err != nil {
		log.Fatal(err)
	}

	err = http.ListenAndServe(":3333", nil)
	if errors.Is(err, http.ErrServerClosed) {
		fmt.Printf("server closed\n")
	} else if err != nil {
		fmt.Printf("error starting server: %s\n", err)
		os.Exit(1)
	}
}

func UpdateAndView(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
        http.NotFound(w, r)
        return
    }
	err := Count()
	if err != nil {
		io.WriteString(w, Wrap(err, "Error counting").Error())
	} else {
	count, err := GetCount()
	fmt.Printf("got / request\n")
	if err != nil {
		io.WriteString(w, Wrap(err, "Error fetching data").Error())
	} else {
		io.WriteString(w, fmt.Sprintf("%d Website Visit!\n", count))
	}
	}
}

func Count() error {
	ctx := context.Background()
	transaction, err := pool.BeginTx(ctx, nil)
	if err != nil {
		return Wrap(err, "failed starting transaction")
	}

	data, _ := json.Marshal(MetricData{})
	err = setCount(transaction, MetricTable, data)
	if err != nil {
		if errorTransaction := transaction.Rollback(); errorTransaction != nil {
			return Wrap(err, errorTransaction.Error()+" error rolling back transaction")
		}
		return Wrap(err, "error inserting payment")
	}

	return transaction.Commit()
}

func GetCount() (int, error) {
	count, err := getCount(pool, MetricTable)
	return count.(int), err
}


func getCount(transaction *sql.DB, table string) (interface{}, error) {
		// Insert
		fetchQuery :=
			fmt.Sprintf(`SELECT id FROM %q ORDER BY createdAt DESC LIMIT 1;`, MetricTable)
		rows, err := pool.Query(fetchQuery)
		if err != nil {
			return nil, Wrap(err, "failed to execute insert data statement")
		}
		defer rows.Close()
	
		var count int
		rw := rows.Next()
		if rw {
		err = rows.Scan(&count)
		return count, err 
		}
		return nil, errors.New("no data found")
}

func setCount(transaction *sql.Tx, table string, value interface{}) error {
		// Insert
		insertQuery :=
			fmt.Sprintf(`INSERT INTO %q(data, meta) VALUES($1, $2);`, table)
		_, err := execQuery(transaction, insertQuery, value, value)
		if err != nil {
			return Wrap(err, "failed to execute insert data statement")
		}
	
		return nil
}

func getHello(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("got /hello request\n")
	io.WriteString(w, "Hello, HTTP!\n")
}

// DatabaseConfigBanking holds db configurations.
type DatabaseConfigBanking struct {
	Host     string `env:"DATABASE_HOST" envDefault:"localhost"`
	Port     string `env:"DATABASE_PORT" envDefault:"5432"`
	Database string `env:"BANKING_DATABASE_NAME" envDefault:"banking"`
	User     string `env:"DATABASE_USER" envDefault:"dev"`
	Password string `env:"DATABASE_PASSWORD" envDefault:"none"`
}

func (conf DatabaseConfigBanking) DSN() string {
	connString := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		conf.User,
		conf.Password,
		conf.Host,
		conf.Port,
		conf.Database,
	)

	return connString
}

// nolint
func execQuery(transaction *sql.Tx, userQuery string, params ...interface{}) (sql.Result, error) {
	statement, err := transaction.Prepare(userQuery)
	if err != nil {
		return nil, Wrap(err, "failed preparing query")
	}
	if statement != nil {
		defer statement.Close()
	} else {
		return nil, errors.New("transaction state is nil")
	}
	result, err := statement.Exec(params...)
	if err != nil {
		return nil, Wrap(err, "failed to execute statement")
	}
	return result, nil
}

func Wrap(err error, msg string) error {
	return fmt.Errorf("%s\n caused by:%w", msg, err)
}

type MetricData struct {
	Id *int
	CreatedAt string
	Data interface{}
	Meta interface{}
}