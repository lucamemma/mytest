package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// --- Test Mock Implementations ---

type MockResult struct{ mock.Mock }

func (m *MockResult) LastInsertId() (int64, error) {
	args := m.Called()
	return args.Get(0).(int64), args.Error(1)
}
func (m *MockResult) RowsAffected() (int64, error) {
	args := m.Called()
	return args.Get(0).(int64), args.Error(1)
}

type MockRow struct{ mock.Mock }

func (m *MockRow) Scan(dest ...interface{}) error {
	args := m.Called(dest...)
	return args.Error(0)
}

type MockRows struct{ mock.Mock }

func (m *MockRows) Next() bool {
	args := m.Called()
	return args.Bool(0)
}
func (m *MockRows) Scan(dest ...interface{}) error {
	args := m.Called(dest...)
	return args.Error(0)
}
func (m *MockRows) Close() error {
	args := m.Called()
	return args.Error(0)
}
func (m *MockRows) Err() error {
	args := m.Called()
	return args.Error(0)
}

type MockTx struct{ mock.Mock }

func (m *MockTx) QueryRow(query string, args ...interface{}) RowLike {
	allArgs := append([]interface{}{query}, args...)
	ret := m.Called(allArgs...)
	return ret.Get(0).(RowLike)
}
func (m *MockTx) Exec(query string, args ...interface{}) (sql.Result, error) {
	allArgs := append([]interface{}{query}, args...)
	ret := m.Called(allArgs...)
	return ret.Get(0).(sql.Result), ret.Error(1)
}
func (m *MockTx) Commit() error {
	ret := m.Called()
	return ret.Error(0)
}
func (m *MockTx) Rollback() error {
	ret := m.Called()
	return ret.Error(0)
}
func (m *MockTx) Query(query string, args ...interface{}) (RowsLike, error) {
	allArgs := append([]interface{}{query}, args...)
	ret := m.Called(allArgs...)
	return ret.Get(0).(RowsLike), ret.Error(1)
}

type MockDB struct{ mock.Mock }

func (m *MockDB) Begin() (TxExecutor, error) {
	ret := m.Called()
	return ret.Get(0).(TxExecutor), ret.Error(1)
}
func (m *MockDB) QueryRow(query string, args ...interface{}) RowLike {
	allArgs := append([]interface{}{query}, args...)
	ret := m.Called(allArgs...)
	return ret.Get(0).(RowLike)
}
func (m *MockDB) Query(query string, args ...interface{}) (RowsLike, error) {
	allArgs := append([]interface{}{query}, args...)
	ret := m.Called(allArgs...)
	return ret.Get(0).(RowsLike), ret.Error(1)
}
func (m *MockDB) Exec(query string, args ...interface{}) (sql.Result, error) {
	allArgs := append([]interface{}{query}, args...)
	ret := m.Called(allArgs...)
	return ret.Get(0).(sql.Result), ret.Error(1)
}

// --- Unit Tests for HTTP Handlers ---

func TestCreateOrderHandler_Success(t *testing.T) {
	mockDB := &MockDB{}
	mockTx := &MockTx{}

	mockDB.On("Begin").Return(mockTx, nil)

	mockTx.On("Rollback").Return(nil)
	mockTx.On("Commit").Return(nil)

	mockResult := new(MockResult)
	mockResult.On("RowsAffected").Return(int64(1), nil)
	mockTx.On("Exec", "INSERT INTO orders (order_id, total_price, vat_amount, created_at) VALUES ($1, $2, $3, $4)", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(mockResult, nil).Once()

	mockRow1 := &MockRow{}
	mockRow1.On("Scan", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		*(args.Get(0).(*int)) = 1
		*(args.Get(1).(*string)) = "Laptop Pro"
		*(args.Get(2).(*float64)) = 1200.00
		*(args.Get(3).(*float64)) = 0.22
	}).Return(nil)
	mockTx.On("QueryRow", "SELECT id, name, price, vat_rate FROM products WHERE id = $1", 1).Return(mockRow1)

	// Mock GetProductByID for the second product.
	mockRow2 := &MockRow{}
	mockRow2.On("Scan", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		*(args.Get(0).(*int)) = 2
		*(args.Get(1).(*string)) = "Keyboard"
		*(args.Get(2).(*float64)) = 150.00
		*(args.Get(3).(*float64)) = 0.22
	}).Return(nil)
	mockTx.On("QueryRow", "SELECT id, name, price, vat_rate FROM products WHERE id = $1", 2).Return(mockRow2)

	mockItemRow := &MockRow{}
	mockItemRow.On("Scan", mock.Anything).Run(func(args mock.Arguments) {
		*(args.Get(0).(*int)) = 1 // Return some item ID
	}).Return(nil)
	insertItemSQL := `
	INSERT INTO order_items (order_id, product_id, quantity, unit_price, item_vat)
	VALUES ($1, $2, $3, $4, $5)
	RETURNING item_id;`
	mockTx.On("QueryRow", insertItemSQL, mock.Anything, 1, 1, 1200.00, 264.0).Return(mockItemRow).Once()
	mockTx.On("QueryRow", insertItemSQL, mock.Anything, 2, 2, 150.00, 66.0).Return(mockItemRow).Once()

	mockTx.On("Exec", "UPDATE orders SET total_price = $1, vat_amount = $2 WHERE order_id = $3", 1500.00, 330.00, mock.Anything).Return(mockResult, nil).Once()

	orderPayload := IncomingOrder{
		Items: []IncomingOrderItem{
			{ProductID: 1, Quantity: 1},
			{ProductID: 2, Quantity: 2},
		},
	}
	body, _ := json.Marshal(orderPayload)
	req := httptest.NewRequest("POST", "/orders", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	handler := createOrderHandler(mockDB)
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Code)
	var responseOrder OutgoingOrder
	err := json.NewDecoder(rr.Body).Decode(&responseOrder)
	assert.NoError(t, err)
	assert.NotEmpty(t, responseOrder.OrderID)
	assert.InDelta(t, 1500.00, responseOrder.TotalOrderPrice, 0.001)
	assert.InDelta(t, 330.00, responseOrder.VATAmount, 0.001)
	assert.Len(t, responseOrder.Items, 2)

	mockDB.AssertExpectations(t)
	mockTx.AssertExpectations(t)
}

func TestGetProductsHandler_Success(t *testing.T) {
	mockDB := &MockDB{}
	mockRows := &MockRows{}

	// This test will now use the testify/mock objects from main.go
	mockDB.On("Query", "SELECT id, name, price, vat_rate FROM products").Return(mockRows, nil)
	mockRows.On("Next").Return(false) // No rows
	mockRows.On("Close").Return(nil)
	mockRows.On("Err").Return(nil)

	req := httptest.NewRequest("GET", "/products", nil)
	rr := httptest.NewRecorder()
	handler := getProductsHandler(mockDB)
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.JSONEq(t, `[]`, rr.Body.String())

	mockDB.AssertExpectations(t)
	mockRows.AssertExpectations(t)
}

func TestGetOrderHandler_NotFound(t *testing.T) {
	mockDB := &MockDB{}
	mockRow := &MockRow{}

	mockRow.On("Scan", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(sql.ErrNoRows)

	mockDB.On("QueryRow", "SELECT order_id, total_price, vat_amount, created_at FROM orders WHERE order_id = $1", "nonexistent-order").Return(mockRow)

	req := httptest.NewRequest("GET", "/orders/nonexistent-order", nil)
	rr := httptest.NewRecorder()

	router := mux.NewRouter()
	router.HandleFunc("/orders/{id}", getOrderHandler(mockDB))
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "Order not found")

	mockDB.AssertExpectations(t)
	mockRow.AssertExpectations(t)
}

func TestCreateOrderHandler_ProductNotFound(t *testing.T) {
	mockDB := &MockDB{}
	mockTx := &MockTx{}

	mockDB.On("Begin").Return(mockTx, nil)
	mockTx.On("Rollback").Return(nil)

	mockResult := new(MockResult)
	mockResult.On("RowsAffected").Return(int64(1), nil)
	mockTx.On("Exec", mock.AnythingOfType("string"), mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(mockResult, nil).Once()

	mockRow := new(MockRow)
	mockRow.On("Scan", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(sql.ErrNoRows)
	mockTx.On("QueryRow", "SELECT id, name, price, vat_rate FROM products WHERE id = $1", 999).Return(mockRow)

	// not existing product
	orderPayload := IncomingOrder{
		Items: []IncomingOrderItem{{ProductID: 999, Quantity: 1}},
	}
	body, _ := json.Marshal(orderPayload)
	req := httptest.NewRequest("POST", "/orders", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	handler := createOrderHandler(mockDB)
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "Product with ID 999 not found")

	mockDB.AssertExpectations(t)
	mockTx.AssertExpectations(t)
	mockRow.AssertExpectations(t)
}
