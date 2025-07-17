# Mytest Subito Project

## Intro
This project is a RESTful API service written in Go for managing customer orders from a cart.

## Features
Contains the following features:

- Create an Order: POST /order
- Welcome Endpoint: GET /
- List Products: GET /products (for manual testing)
- Get an Order by ID: GET /orders/{id}
- Default 404 Handler: All undefined routes return a clean JSON "Not Found" error.

## Architectural Decisions
This project was built starting from the four commands shown in the last sections, to better interpolate what was expected to be a working result. The commands point towards a single  web server image running a Go app, given that there is no compose command. This means a docker-compose.yml would be ignored.
For these reasons all is designed to have no real database interactions while running. The whole data will be stored in-memory for as long as the server is running (and with mock interactions during the Unit Tests).

### 1. Decoupling with Interfaces (DBExecutor)
The core of the application's design is the use of interfaces (DBExecutor, TxExecutor, RowLike, RowsLike) to abstract away the concrete database/sql package.
The reason behind this decision is for testability. 
By having our business logic (e.g. createOrderHandler) depend on an interface instead of a concrete database connection, we can easily swap out the real database with a mock implementation during tests. 
This allows us to test our application's logic without needing to connect to a live database, making tests fast, reliable, and independent of external services.

### 2. Dual Testing Strategy: Mocks vs. In-Memory DB
The project uses two distinct types of "fake" databases for different testing purposes:

- Unit Testing (main_test.go): The unit tests use mock objects created with the testify/mock library. 

- Manual & Runtime Testing (run.sh): When the application is run via ./scripts/run.sh, it starts up in a special "mock mode" triggered by the DB_HOST=mock environment variable. In this mode, it uses a stateful in-memory database (InMemoryStore).

### 3. Request and response
Starting from the request and response examples given, the *product_id* is defined as an integer (>0). The *quantity* as well is defined as an integer considering items that can only be sold in their entirety. 
The response numeric values such as *vat*, *price*, *order_vat*, *order_price* are handled as two digits floating numbers keeping in mind they represent a currency value (english format).


## Prerequisites
This project needs Docker installed and running.

### 1. Build the Docker Image
`docker build -t mytest .`

### 2. Build the Application
`docker run -v $(pwd):/mnt -w /mnt mytest ./scripts/build.sh`

### 3. Run Unit Tests
`docker run -v $(pwd):/mnt -w /mnt mytest ./scripts/test.sh`

### 4. Run the Server for Manual Testing
`docker run -v $(pwd):/mnt -p 9090:9090 -w /mnt mytest ./scripts/run.sh`

The API will be available at http://localhost:9090 as requested in the specifications.

You can get a list of products (IDs 1, 2, 3, 4, 5) at http://localhost:9090/products.

You can create new orders by sending a POST request to http://localhost:9090/orders.