package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
)

// --- Struct Definitions ---

// simulated catalog product for API request/response.
type Product struct {
	ID      int     `json:"id"`
	Name    string  `json:"name"`
	Price   float64 `json:"price"`
	VATRate float64 `json:"vat_rate"` // VAT rate, e.g., 0.22 for 22%
}

// DBProduct 'products' table in the database.
type DBProduct struct {
	ID      int
	Name    string
	Price   float64
	VATRate float64
}

// IncomingOrderItem represents an item in request body.
type IncomingOrderItem struct {
	ProductID int `json:"product_id"`
	Quantity  int `json:"quantity"`
}

// item in the response body
type OutgoingOrderItem struct {
	ProductID int     `json:"product_id"`
	Quantity  int     `json:"quantity"`
	Price     float64 `json:"price"`
	ItemVAT   float64 `json:"vat"`
}

// order structure
type IncomingOrder struct {
	Items []IncomingOrderItem `json:"items"` // A list of items in the order
}

// order structure as returned in the response body,
type OutgoingOrder struct {
	OrderID         string              `json:"order_id"`
	TotalOrderPrice float64             `json:"order_price"`
	VATAmount       float64             `json:"order_vat"`
	Items           []OutgoingOrderItem `json:"items"`
}

// a row in the 'orders' table.
type OrderRecord struct {
	OrderID    string
	TotalPrice float64
	VATAmount  float64
	CreatedAt  time.Time
}

// a row in the 'order_items' table.
type OrderItemRecord struct {
	ItemID    int // SERIAL PRIMARY KEY in DB, so it's auto-generated
	OrderID   string
	ProductID int
	Quantity  int
	UnitPrice float64
	ItemVAT   float64
}

// RowLike abstracts the behavior of *sql.Row.
type RowLike interface {
	Scan(dest ...interface{}) error
}

// RowsLike abstracts the behavior of *sql.Rows.
type RowsLike interface {
	Next() bool
	Scan(dest ...interface{}) error
	Close() error
	Err() error
}

// TxExecutor defines the methods needed from a transaction for our functions.
type TxExecutor interface {
	QueryRow(query string, args ...interface{}) RowLike
	Exec(query string, args ...interface{}) (sql.Result, error)
	Commit() error
	Rollback() error
	Query(query string, args ...interface{}) (RowsLike, error)
}

// DBExecutor defines the methods needed from a database connection for our functions.
type DBExecutor interface {
	Begin() (TxExecutor, error)
	QueryRow(query string, args ...interface{}) RowLike
	Query(query string, args ...interface{}) (RowsLike, error)
	Exec(query string, args ...interface{}) (sql.Result, error)
}

// --- Database Adapters for Real DB ---
type sqlTxAdapter struct{ *sql.Tx }

func (tx *sqlTxAdapter) QueryRow(query string, args ...interface{}) RowLike {
	return tx.Tx.QueryRow(query, args...)
}
func (tx *sqlTxAdapter) Query(query string, args ...interface{}) (RowsLike, error) {
	return tx.Tx.Query(query, args...)
}
func (tx *sqlTxAdapter) Exec(query string, args ...interface{}) (sql.Result, error) {
	return tx.Tx.Exec(query, args...)
}

type sqlDBAdapter struct{ *sql.DB }

func (db *sqlDBAdapter) Begin() (TxExecutor, error) {
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	return &sqlTxAdapter{tx}, nil
}
func (db *sqlDBAdapter) QueryRow(query string, args ...interface{}) RowLike {
	return db.DB.QueryRow(query, args...)
}
func (db *sqlDBAdapter) Query(query string, args ...interface{}) (RowsLike, error) {
	return db.DB.Query(query, args...)
}
func (db *sqlDBAdapter) Exec(query string, args ...interface{}) (sql.Result, error) {
	return db.DB.Exec(query, args...)
}

// --- In-Memory Store for Mocking a running DB ---

// implements sql.Result for the in-memory store.
type InMemoryResult struct {
	rowsAffected int64
}

func (r *InMemoryResult) LastInsertId() (int64, error) {
	return 0, errors.New("LastInsertId is not supported in in-memory mock")
}

func (r *InMemoryResult) RowsAffected() (int64, error) {
	return r.rowsAffected, nil
}

// holds data in memory for mock mode/ thread-safe.
type InMemoryStore struct {
	mu         sync.RWMutex
	products   map[int]DBProduct
	orders     map[string]OrderRecord
	orderItems map[string][]OrderItemRecord
	nextItemID int
}

// creates and initializes an in-memory store
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		products:   make(map[int]DBProduct),
		orders:     make(map[string]OrderRecord),
		orderItems: make(map[string][]OrderItemRecord),
		nextItemID: 1,
	}
}

// sample products
func (s *InMemoryStore) Populate() {
	s.products[1] = DBProduct{ID: 1, Name: "Laptop Pro", Price: 1499.99, VATRate: 0.22}
	s.products[2] = DBProduct{ID: 2, Name: "Wireless Mouse", Price: 79.99, VATRate: 0.22}
	s.products[3] = DBProduct{ID: 3, Name: "Mechanical Keyboard", Price: 129.99, VATRate: 0.22}
	s.products[4] = DBProduct{ID: 4, Name: "4K Monitor", Price: 649.50, VATRate: 0.22}
	s.products[5] = DBProduct{ID: 5, Name: "HD Monitor", Price: 150.50, VATRate: 0.15}
}

// mock implementation of DBExecutor that uses the in-memory store
type InMemoryDB struct {
	store *InMemoryStore
}

func (db *InMemoryDB) Begin() (TxExecutor, error) {
	return &InMemoryTx{store: db.store}, nil
}

func (db *InMemoryDB) Query(query string, args ...interface{}) (RowsLike, error) {
	db.store.mu.RLock()
	defer db.store.mu.RUnlock()

	if query == "SELECT id, name, price, vat_rate FROM products" {
		rows := &InMemoryRows{}
		for _, p := range db.store.products {
			rows.data = append(rows.data, []interface{}{p.ID, p.Name, p.Price, p.VATRate})
		}
		return rows, nil
	}
	if query == "SELECT product_id, quantity, unit_price, item_vat FROM order_items WHERE order_id = $1" {
		orderID := args[0].(string)
		rows := &InMemoryRows{}
		if items, ok := db.store.orderItems[orderID]; ok {
			for _, item := range items {
				rows.data = append(rows.data, []interface{}{item.ProductID, item.Quantity, item.UnitPrice, item.ItemVAT})
			}
		}
		return rows, nil
	}
	return nil, fmt.Errorf("in-memory mock for DB.Query not implemented: %s", query)
}

func (db *InMemoryDB) QueryRow(query string, args ...interface{}) RowLike {
	db.store.mu.RLock()
	defer db.store.mu.RUnlock()

	if query == "SELECT order_id, total_price, vat_amount, created_at FROM orders WHERE order_id = $1" {
		orderID := args[0].(string)
		if order, ok := db.store.orders[orderID]; ok {
			return &InMemoryRow{data: []interface{}{order.OrderID, order.TotalPrice, order.VATAmount, order.CreatedAt}, err: nil}
		}
		return &InMemoryRow{err: sql.ErrNoRows}
	}

	return &InMemoryRow{err: fmt.Errorf("in-memory mock for DB.QueryRow not implemented: %s", query)}
}

func (db *InMemoryDB) Exec(query string, args ...interface{}) (sql.Result, error) {
	return nil, errors.New("exec should be called on a transaction, not directly on the DB")
}

// mock implementation of TxExecutor.
type InMemoryTx struct {
	store *InMemoryStore
}

func (tx *InMemoryTx) Commit() error   { return nil } // No-op for in-memory
func (tx *InMemoryTx) Rollback() error { return nil } // No-op for in-memory

func (tx *InMemoryTx) Query(query string, args ...interface{}) (RowsLike, error) {
	// Delegate to the main DB query method for simplicity
	return (&InMemoryDB{store: tx.store}).Query(query, args...)
}

func (tx *InMemoryTx) QueryRow(query string, args ...interface{}) RowLike {
	tx.store.mu.Lock()
	defer tx.store.mu.Unlock()

	if query == "SELECT id, name, price, vat_rate FROM products WHERE id = $1" {
		productID := args[0].(int)
		if p, ok := tx.store.products[productID]; ok {
			return &InMemoryRow{data: []interface{}{p.ID, p.Name, p.Price, p.VATRate}, err: nil}
		}
		return &InMemoryRow{err: sql.ErrNoRows}
	}

	sqlStatement := `
	INSERT INTO order_items (order_id, product_id, quantity, unit_price, item_vat)
	VALUES ($1, $2, $3, $4, $5)
	RETURNING item_id;`
	if query == sqlStatement {
		orderID := args[0].(string)
		item := OrderItemRecord{
			ItemID:    tx.store.nextItemID,
			OrderID:   orderID,
			ProductID: args[1].(int),
			Quantity:  args[2].(int),
			UnitPrice: args[3].(float64),
			ItemVAT:   args[4].(float64),
		}
		tx.store.orderItems[orderID] = append(tx.store.orderItems[orderID], item)
		tx.store.nextItemID++
		return &InMemoryRow{data: []interface{}{item.ItemID}, err: nil}
	}

	return &InMemoryRow{err: fmt.Errorf("in-memory mock for Tx.QueryRow not implemented: %s", query)}
}

func (tx *InMemoryTx) Exec(query string, args ...interface{}) (sql.Result, error) {
	tx.store.mu.Lock()
	defer tx.store.mu.Unlock()

	if query == "INSERT INTO orders (order_id, total_price, vat_amount, created_at) VALUES ($1, $2, $3, $4)" {
		order := OrderRecord{
			OrderID:    args[0].(string),
			TotalPrice: args[1].(float64),
			VATAmount:  args[2].(float64),
			CreatedAt:  args[3].(time.Time),
		}
		tx.store.orders[order.OrderID] = order
		return &InMemoryResult{rowsAffected: 1}, nil
	}

	if query == "UPDATE orders SET total_price = $1, vat_amount = $2 WHERE order_id = $3" {
		orderID := args[2].(string)
		if order, ok := tx.store.orders[orderID]; ok {
			order.TotalPrice = args[0].(float64)
			order.VATAmount = args[1].(float64)
			tx.store.orders[orderID] = order
			return &InMemoryResult{rowsAffected: 1}, nil
		}
		return nil, fmt.Errorf("order not found for update: %s", orderID)
	}

	return nil, fmt.Errorf("in-memory mock for Exec not implemented: %s", query)
}

// mock implementation of RowLike.
type InMemoryRow struct {
	data []interface{}
	err  error
}

func (r *InMemoryRow) Scan(dest ...interface{}) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.data) {
		return fmt.Errorf("scan error: expected %d dest values, got %d", len(r.data), len(dest))
	}
	for i, val := range r.data {
		switch d := dest[i].(type) {
		case *string:
			*d = val.(string)
		case *float64:
			*d = val.(float64)
		case *int:
			*d = val.(int)
		case *time.Time:
			*d = val.(time.Time)
		default:
			return fmt.Errorf("unsupported type for scan: %T", d)
		}
	}
	return nil
}

// mock implementation of RowsLike.
type InMemoryRows struct {
	data         [][]interface{}
	currentIndex int
}

func (r *InMemoryRows) Next() bool {
	r.currentIndex++
	return r.currentIndex <= len(r.data)
}

func (r *InMemoryRows) Scan(dest ...interface{}) error {
	if r.currentIndex > len(r.data) || r.currentIndex == 0 {
		return errors.New("scan called out of bounds or before Next")
	}
	rowData := r.data[r.currentIndex-1]
	row := &InMemoryRow{data: rowData}
	return row.Scan(dest...)
}
func (r *InMemoryRows) Close() error { return nil }
func (r *InMemoryRows) Err() error   { return nil }

func main() {
	var dbExecutor DBExecutor

	// Check for mock mode, required by the testing workflow.
	if os.Getenv("DB_HOST") == "mock" {
		log.Println("--- RUNNING IN MOCK DATABASE MODE (STATEFUL) ---")
		store := NewInMemoryStore()
		store.Populate()
		dbExecutor = &InMemoryDB{store: store}
	} else {
		log.Println("--- RUNNING IN LIVE DATABASE MODE ---")
		// Retrieve database connection details from environment variables.
		dbHost := os.Getenv("DB_HOST")
		dbName := os.Getenv("DB_NAME")
		dbUser := os.Getenv("DB_USER")
		dbPassword := os.Getenv("DB_PASSWORD")

		connStr := fmt.Sprintf("host=%s user=%s password=%s dbname=%s sslmode=disable",
			dbHost, dbUser, dbPassword, dbName)

		var err error
		var db *sql.DB

		for i := 0; i < 10; i++ {
			db, err = sql.Open("postgres", connStr)
			if err != nil {
				log.Printf("Error opening database: %v. Retrying in 5 seconds...", err)
				time.Sleep(5 * time.Second)
				continue
			}
			err = db.Ping()
			if err != nil {
				log.Printf("Error connecting to the database: %v. Retrying in 5 seconds...", err)
				db.Close()
				time.Sleep(5 * time.Second)
				continue
			}
			log.Println("Successfully connected to the database!")
			break
		}

		if err != nil {
			log.Fatalf("Could not connect to the database after multiple retries: %v", err)
		}
		defer db.Close()

		// Wrap the real DB connection in our adapter.
		dbExecutor = &sqlDBAdapter{db}
	}

	router := mux.NewRouter()

	// API Routes - Pass the chosen executor (real or mock) to the handlers.
	router.NotFoundHandler = http.HandlerFunc(notFoundHandler)

	router.HandleFunc("/", homeHandler).Methods("GET")
	router.HandleFunc("/products", getProductsHandler(dbExecutor)).Methods("GET")
	router.HandleFunc("/order", createOrderHandler(dbExecutor)).Methods("POST")
	router.HandleFunc("/orders/{id}", getOrderHandler(dbExecutor)).Methods("GET")

	port := os.Getenv("PORT")
	if port == "" {
		port = "9090" // Default port
	}

	log.Printf("Server starting on port %s...", port)
	log.Fatal(http.ListenAndServe(":"+port, router))
}

// responds to the root URL with a welcome message.
func homeHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Welcome to the Subito Project Order API!"})
}

// all requests to undefined routes.
func notFoundHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]string{"error": "Not Found"})
}

// --- Product Database Functions ---

// fetches a single product from the 'products' table by its ID
func GetProductByID(executor TxExecutor, productID int) (*DBProduct, error) {
	var product DBProduct
	row := executor.QueryRow("SELECT id, name, price, vat_rate FROM products WHERE id = $1", productID)
	err := row.Scan(&product.ID, &product.Name, &product.Price, &product.VATRate)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Wrapping the error is good practice to provide more context.
			return nil, fmt.Errorf("product not found: %w", sql.ErrNoRows)
		}
		return nil, fmt.Errorf("failed to scan product: %w", err)
	}
	return &product, nil
}

// fetches all products from the 'products' table
func GetAllProducts(executor DBExecutor) ([]DBProduct, error) {
	rows, err := executor.Query("SELECT id, name, price, vat_rate FROM products")
	if err != nil {
		return nil, fmt.Errorf("failed to query products: %w", err)
	}
	defer rows.Close()

	var products []DBProduct
	for rows.Next() {
		var product DBProduct
		if err := rows.Scan(&product.ID, &product.Name, &product.Price, &product.VATRate); err != nil {
			return nil, fmt.Errorf("failed to scan product row: %w", err)
		}
		products = append(products, product)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error during products iteration: %w", err)
	}
	return products, nil
}

// inserts a new order record into the 'orders' table
func InsertOrder(executor TxExecutor, order *OrderRecord) error {
	_, err := executor.Exec("INSERT INTO orders (order_id, total_price, vat_amount, created_at) VALUES ($1, $2, $3, $4)",
		order.OrderID, order.TotalPrice, order.VATAmount, order.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to insert order: %w", err)
	}
	return nil
}

// updates the total_price and vat_amount for an existing order
func UpdateOrderTotals(executor TxExecutor, orderID string, totalPrice, vatAmount float64) error {
	_, err := executor.Exec("UPDATE orders SET total_price = $1, vat_amount = $2 WHERE order_id = $3",
		toFixed(totalPrice, 2), toFixed(vatAmount, 2), orderID)
	if err != nil {
		return fmt.Errorf("failed to update order totals: %w", err)
	}
	return nil
}

// fetches a complete order by its ID, including its items.
func GetOrderByID(executor DBExecutor, orderID string) (*OutgoingOrder, error) {
	var orderRecord OrderRecord
	row := executor.QueryRow("SELECT order_id, total_price, vat_amount, created_at FROM orders WHERE order_id = $1", orderID)
	err := row.Scan(&orderRecord.OrderID, &orderRecord.TotalPrice, &orderRecord.VATAmount, &orderRecord.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Wrapping the error is good practice to provide more context.
			return nil, fmt.Errorf("order not found: %w", sql.ErrNoRows)
		}
		return nil, fmt.Errorf("failed to scan order: %w", err)
	}

	items, err := GetOrderItemsByOrderID(executor, orderID)
	if err != nil {
		return nil, fmt.Errorf("failed to get order items for order %s: %w", orderID, err)
	}

	outgoingOrder := &OutgoingOrder{
		OrderID:         orderRecord.OrderID,
		TotalOrderPrice: orderRecord.TotalPrice,
		VATAmount:       orderRecord.VATAmount,
		Items:           items,
	}
	return outgoingOrder, nil
}

// --- Order Item Database Functions ---

// inserts a new order item record into the 'order_items' table.
func InsertOrderItem(executor TxExecutor, item *OrderItemRecord) (int, error) {
	var itemID int
	sqlStatement := `
	INSERT INTO order_items (order_id, product_id, quantity, unit_price, item_vat)
	VALUES ($1, $2, $3, $4, $5)
	RETURNING item_id;`

	err := executor.QueryRow(sqlStatement, item.OrderID, item.ProductID, item.Quantity, item.UnitPrice, item.ItemVAT).Scan(&itemID)
	if err != nil {
		return 0, fmt.Errorf("failed to insert order item: %w", err)
	}
	return itemID, nil
}

// GetOrderItemsByOrderID fetches all items for a given order ID.
func GetOrderItemsByOrderID(executor DBExecutor, orderID string) ([]OutgoingOrderItem, error) {
	rows, err := executor.Query("SELECT product_id, quantity, unit_price, item_vat FROM order_items WHERE order_id = $1", orderID)
	if err != nil {
		return nil, fmt.Errorf("failed to query order items: %w", err)
	}
	defer rows.Close()

	var items []OutgoingOrderItem
	for rows.Next() {
		var item OutgoingOrderItem
		if err := rows.Scan(&item.ProductID, &item.Quantity, &item.Price, &item.ItemVAT); err != nil {
			return nil, fmt.Errorf("failed to scan order item row: %w", err)
		}
		items = append(items, item)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error during order items iteration: %w", err)
	}
	return items, nil
}

// --- HTTP Handlers ---

// returns an http.HandlerFunc that uses the provided DBExecutor. for manual tests
func getProductsHandler(executor DBExecutor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		products, err := GetAllProducts(executor)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to retrieve products: %v", err), http.StatusInternalServerError)
			return
		}
		publicProducts := []Product{}
		for _, p := range products {
			publicProducts = append(publicProducts, Product{
				ID:      p.ID,
				Name:    p.Name,
				Price:   p.Price,
				VATRate: p.VATRate,
			})
		}
		if publicProducts == nil {
			publicProducts = []Product{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(publicProducts)
	}
}

// returns an http.HandlerFunc that uses the provided DBExecutor.
func createOrderHandler(executor DBExecutor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var incomingOrder IncomingOrder
		if err := json.NewDecoder(r.Body).Decode(&incomingOrder); err != nil {
			http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
			return
		}

		if len(incomingOrder.Items) == 0 {
			http.Error(w, "Order must contain at least one item", http.StatusBadRequest)
			return
		}

		tx, err := executor.Begin()
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to begin transaction: %v", err), http.StatusInternalServerError)
			return
		}
		defer tx.Rollback() // Rollback is a safeguard

		orderID := uuid.New().String()
		var totalOrderPrice float64
		var vatAmount float64
		outgoingItems := []OutgoingOrderItem{}

		orderRecord := &OrderRecord{
			OrderID:    orderID,
			TotalPrice: 0.0,
			VATAmount:  0.0,
			CreatedAt:  time.Now(),
		}
		if err := InsertOrder(tx, orderRecord); err != nil {
			http.Error(w, fmt.Sprintf("Failed to insert order: %v", err), http.StatusInternalServerError)
			return
		}

		for _, item := range incomingOrder.Items {
			product, err := GetProductByID(tx, item.ProductID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					http.Error(w, fmt.Sprintf("Product with ID %d not found", item.ProductID), http.StatusNotFound)
				} else {
					http.Error(w, fmt.Sprintf("Database error fetching product %d: %v", item.ProductID, err), http.StatusInternalServerError)
				}
				return
			}

			if item.Quantity <= 0 {
				http.Error(w, fmt.Sprintf("Quantity for product %d must be positive", item.ProductID), http.StatusBadRequest)
				return
			}

			itemTotalPrice := product.Price * float64(item.Quantity)
			itemVAT := product.Price * product.VATRate

			totalOrderPrice += itemTotalPrice
			vatAmount += itemVAT * float64(item.Quantity)

			outgoingItems = append(outgoingItems, OutgoingOrderItem{
				ProductID: item.ProductID,
				Quantity:  item.Quantity,
				Price:     product.Price,
				ItemVAT:   toFixed(itemVAT, 2),
			})

			orderItemRecord := &OrderItemRecord{
				OrderID:   orderID,
				ProductID: item.ProductID,
				Quantity:  item.Quantity,
				UnitPrice: product.Price,
				ItemVAT:   toFixed(itemVAT, 2),
			}
			if _, err := InsertOrderItem(tx, orderItemRecord); err != nil {
				http.Error(w, fmt.Sprintf("Failed to insert order item: %v", err), http.StatusInternalServerError)
				return
			}
		}

		if err := UpdateOrderTotals(tx, orderID, totalOrderPrice, vatAmount); err != nil {
			http.Error(w, fmt.Sprintf("Failed to update order totals: %v", err), http.StatusInternalServerError)
			return
		}

		if err := tx.Commit(); err != nil {
			http.Error(w, fmt.Sprintf("Failed to commit transaction: %v", err), http.StatusInternalServerError)
			return
		}

		outgoingOrder := OutgoingOrder{
			OrderID:         orderID,
			TotalOrderPrice: toFixed(totalOrderPrice, 2),
			VATAmount:       toFixed(vatAmount, 2),
			Items:           outgoingItems,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(outgoingOrder)
	}
}

// returns an http.HandlerFunc that uses the provided DBExecutor. for manual tests
func getOrderHandler(executor DBExecutor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		orderID := vars["id"]

		order, err := GetOrderByID(executor, orderID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "Order not found", http.StatusNotFound)
			} else {
				http.Error(w, fmt.Sprintf("Failed to retrieve order: %v", err), http.StatusInternalServerError)
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(order)
	}
}

// float helpers for currency formatting
func round(num float64) int {
	return int(num + math.Copysign(0.5, num))
}

func toFixed(num float64, precision int) float64 {
	output := math.Pow(10, float64(precision))
	return float64(round(num*output)) / output
}
