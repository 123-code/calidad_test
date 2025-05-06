package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "github.com/lib/pq"
)

var testApp *App
var testDB *sql.DB

func TestMain(m *testing.M) {
	// Set up a connection to your test database
	// REPLACE THIS WITH YOUR ACTUAL TEST DATABASE CONNECTION STRING
	connStr := "postgresql://calidadsoftware_user:Pp5IT3eGOO0fPHRx6ubNtUOuEu55zK7q@dpg-d0clc0be5dus73agl78g-a.oregon-postgres.render.com/calidadsoftware"
	var err error
	testDB, err = sql.Open("postgres", connStr)
	if err != nil {
		// Use log.Fatalf in TestMain as it's outside a test function
		log.Fatalf("Error connecting to test database: %v", err)
	}
	defer testDB.Close()

	// Ensure the tasks table exists in the test database
	_, err = testDB.Exec(`CREATE TABLE IF NOT EXISTS tasks (
		id SERIAL PRIMARY KEY,
		title TEXT NOT NULL,
		description TEXT,
		due_date TEXT NOT NULL,
		priority INTEGER NOT NULL CHECK (priority >= 1 AND priority <= 3),
		status TEXT NOT NULL DEFAULT 'Pending'
	)`)
	if err != nil {
		log.Fatalf("Error creating tasks table in test database: %v", err)
	}

	testApp = &App{DB: testDB}

	// Run the tests
	m.Run()
}

// Helper function to create a task directly in the database for test setup
// This helper should now accept and use the transaction
func createTaskInDB(t *testing.T, tx *sql.Tx, task Task) int {
	query := `INSERT INTO tasks (title, description, due_date, priority, status)
              VALUES ($1, $2, $3, $4, $5) RETURNING id`
	var id int
	// Use tx.QueryRow instead of testDB.QueryRow
	err := tx.QueryRow(query, task.Title, task.Description, task.DueDate, task.Priority, task.Status).Scan(&id)
	if err != nil {
		t.Fatalf("Failed to create task in DB: %v", err)
	}
	return id
}

// Helper function to get all tasks directly from the database for verification
// This helper should also accept and use the transaction
func getAllTasksFromDB(t *testing.T, tx *sql.Tx) []Task {
	// Use tx.Query instead of testDB.Query
	rows, err := tx.Query("SELECT id, title, description, due_date, priority, status FROM tasks")
	if err != nil {
		t.Fatalf("Failed to get tasks from DB: %v", err)
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var task Task
		if err := rows.Scan(&task.ID, &task.Title, &task.Description, &task.DueDate, &task.Priority, &task.Status); err != nil {
			t.Fatalf("Failed to scan task from DB: %v", err)
		}
		tasks = append(tasks, task)
	}
	return tasks
}

// Helper function to execute a handler and return the recorder
func executeRequest(req *http.Request, handler http.HandlerFunc) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

// Test Case 1: Eliminate existing task
func TestDeleteExistingTask(t *testing.T) {
	// Use a transaction to isolate this test
	tx, err := testDB.Begin()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback() // Rollback the transaction at the end of the test

	// Create a temporary App instance that uses the transaction's connection
	appWithTx := &App{DB: tx}

	// Create a task to delete using the transaction-aware helper
	taskToCreate := Task{
		Title:       "Task to Delete",
		Description: "This task will be deleted",
		DueDate:     "2024-12-31",
		Priority:    2,
		Status:      "Pending",
	}
	createdID := createTaskInDB(t, tx, taskToCreate)

	// Create a DELETE request for the task
	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("/tasks/%d", createdID), nil)

	// Execute the request using the handler with the transaction-aware app
	rr := executeRequest(req, appWithTx.deleteTask)

	// Assert the status code
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	// Assert the response body (success message)
	expectedMessage := fmt.Sprintf("Task with ID %d deleted successfully", createdID)
	var response map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Could not unmarshal response body: %v", err)
	}
	if msg, ok := response["message"]; !ok || msg != expectedMessage {
		t.Errorf("Handler returned unexpected body: got %v want {\"message\": \"%s\"}", rr.Body.String(), expectedMessage)
	}

	// Verify the task is actually deleted by trying to fetch it
	getReq, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("/tasks/%d", createdID), nil)
	getRR := executeRequest(getReq, appWithTx.getTaskByID) // Use handler with transaction-aware app

	if status := getRR.Code; status != http.StatusNotFound {
		t.Errorf("After deletion, GET handler returned wrong status code: got %v want %v", status, http.StatusNotFound)
	}

	// Assert the error message for not found
	expectedErrorMsg := "tarea no encontrada\n" // http.Error adds a newline
	if getRR.Body.String() != expectedErrorMsg {
		t.Errorf("After deletion, GET handler returned unexpected body: got %q want %q", getRR.Body.String(), expectedErrorMsg)
	}
}

// Test Case 2: Try to eliminate non existing task
func TestDeleteNonExistingTask(t *testing.T) {
	// Use a transaction to isolate this test
	tx, err := testDB.Begin()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback() // Rollback the transaction

	// Create a temporary App instance that uses the transaction's connection
	appWithTx := &App{DB: tx}

	// Choose an ID that definitely does not exist
	nonExistingID := 99999

	// Create a DELETE request for the non-existing task
	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("/tasks/%d", nonExistingID), nil)

	// Execute the request
	rr := executeRequest(req, appWithTx.deleteTask)

	// Assert the status code is 404 Not Found
	if status := rr.Code; status != http.StatusNotFound {
		t.Errorf("Handler returned wrong status code: got %v want %v", status, http.StatusNotFound)
	}

	// Assert the response body is the "tarea no encontrada" message
	expectedErrorMsg := "tarea no encontrada\n" // http.Error adds a newline
	if rr.Body.String() != expectedErrorMsg {
		t.Errorf("Handler returned unexpected body: got %q want %q", rr.Body.String(), expectedErrorMsg)
	}
}

// Test Case 3: Validate other tasks do not get eliminated accidentally
func TestDeleteDoesNotAffectOtherTasks(t *testing.T) {
	// Use a transaction to isolate this test
	tx, err := testDB.Begin()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback() // Rollback the transaction at the end of the test

	// Create a temporary App instance that uses the transaction's connection
	appWithTx := &App{DB: tx}

	// --- ADD THIS LOGGING ---
	initialTasks := getAllTasksFromDB(t, tx) // Use the transaction-aware helper
	t.Logf("Initial tasks in transaction before test setup: %d", len(initialTasks))
	// --- END OF ADDED LOGGING ---

	// Create multiple tasks using the transaction-aware helper
	taskA := Task{Title: "Task A", DueDate: "2024-11-01", Priority: 1}
	taskB := Task{Title: "Task B (to delete)", DueDate: "2024-11-02", Priority: 2}
	taskC := Task{Title: "Task C", DueDate: "2024-11-03", Priority: 3}

	idA := createTaskInDB(t, tx, taskA) // Pass the transaction
	idB := createTaskInDB(t, tx, taskB) // Pass the transaction
	idC := createTaskInDB(t, tx, taskC) // Pass the transaction

	// --- ADD THIS LOGGING ---
	tasksAfterCreation := getAllTasksFromDB(t, tx) // Use the transaction-aware helper
	t.Logf("Tasks in transaction after creation: %d", len(tasksAfterCreation))
	// --- END OF ADDED LOGGING ---

	// Delete Task B
	deleteReq, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("/tasks/%d", idB), nil)
	deleteRR := executeRequest(deleteReq, appWithTx.deleteTask)

	// Assert Task B deletion was successful
	if status := deleteRR.Code; status != http.StatusOK {
		t.Fatalf("Failed to delete Task B: got status %v, body %q", status, deleteRR.Body.String())
	}

	// Get all remaining tasks using the transaction-aware handler
	getAllReq, _ := http.NewRequest(http.MethodGet, "/tasks", nil)
	getAllRR := executeRequest(getAllReq, appWithTx.getAllTasks)

	// Assert status code is 200 OK
	if status := getAllRR.Code; status != http.StatusOK {
		t.Fatalf("Handler returned wrong status code for GET /tasks: got %v want %v", status, http.StatusOK)
	}

	// Unmarshal the response body
	var remainingTasks []Task
	if err := json.Unmarshal(getAllRR.Body.Bytes(), &remainingTasks); err != nil {
		t.Fatalf("Could not unmarshal response body for GET /tasks: %v", err)
	}

	// Check that Task A and Task C are present, and Task B is not
	foundA := false
	foundB := false
	foundC := false

	for _, task := range remainingTasks {
		if task.ID == idA {
			foundA = true
		}
		if task.ID == idB {
			foundB = true
		}
		if task.ID == idC {
			foundC = true
		}
	}

	if !foundA {
		t.Error("Task A was unexpectedly deleted")
	}
	if foundB {
		t.Errorf("Task B (ID %d) was not deleted", idB)
	}
	if !foundC {
		t.Error("Task C was unexpectedly deleted")
	}

	// You can also check the count of remaining tasks
	if len(remainingTasks) != 2 {
		t.Errorf("Expected 2 remaining tasks, but found %d", len(remainingTasks))
	}
}
