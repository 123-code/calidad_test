package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

var testApp *App
var testDB *sql.DB

func TestMain(m *testing.M) {
	// Set up a connection to your test database
	// REPLACE THIS WITH YOUR ACTUAL TEST DATABASE CONNECTION STRING
	connStr := "postgresql://calidadsoftware_user:Pp5IT3eGOO0fPHRx6ubNtUOuEu55zK7q@dpg-d0clc0be5dus73agl78g-a.oregon-postgres.render.com/calidadsoftware_test" // Check the password here!
	var err error
	testDB, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("Error connecting to test database: %v", err)
	}
	// No defer testDB.Close() here, it should be closed after m.Run()

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

	// Clean the table before each test run
	_, err = testDB.Exec("TRUNCATE TABLE tasks RESTART IDENTITY CASCADE")
	if err != nil {
		log.Fatalf("Error truncating tasks table: %v", err)
	}

	testApp = &App{DB: testDB}

	exitCode := m.Run()

	testDB.Close()

	os.Exit(exitCode)
}

func createTaskInDB(t *testing.T, tx *sql.Tx, task Task) int {
	query := `INSERT INTO tasks (title, description, due_date, priority, status)
              VALUES ($1, $2, $3, $4, $5) RETURNING id`
	var id int

	err := tx.QueryRow(query, task.Title, task.Description, task.DueDate, task.Priority, task.Status).Scan(&id)
	if err != nil {
		t.Fatalf("Failed to create task in DB: %v", err)
	}
	return id
}

func getAllTasksFromDB(t *testing.T, tx *sql.Tx) []Task {

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

func executeRequest(req *http.Request, handler http.HandlerFunc) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func TestCreateTask(t *testing.T) {

	tx, err := testDB.Begin()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	appWithTx := &App{DB: tx}

	taskData := Task{
		Title:       "New Task for Test",
		Description: "This is a test task",
		DueDate:     "2024-11-15",
		Priority:    1,
		Status:      "Pending",
	}
	jsonTaskData, _ := json.Marshal(taskData)

	req, _ := http.NewRequest(http.MethodPost, "/tasks", bytes.NewBuffer(jsonTaskData))
	req.Header.Set("Content-Type", "application/json")

	rr := executeRequest(req, appWithTx.createTask)

	if status := rr.Code; status != http.StatusCreated {
		t.Errorf("Handler returned wrong status code: got %v want %v", status, http.StatusCreated)
		t.Errorf("Response body: %s", rr.Body.String())
	}

	var createdTask Task
	if err := json.Unmarshal(rr.Body.Bytes(), &createdTask); err != nil {
		t.Fatalf("Could not unmarshal response body: %v", err)
	}

	if createdTask.ID == 0 {
		t.Error("Created task ID was not returned or is 0")
	}
	if createdTask.Title != taskData.Title || createdTask.Description != taskData.Description ||
		createdTask.DueDate != taskData.DueDate || createdTask.Priority != taskData.Priority ||
		createdTask.Status != taskData.Status {
		t.Errorf("Created task data mismatch. Got %+v, want %+v", createdTask, taskData)
	}

	var dbTask Task
	query := `SELECT id, title, description, due_date, priority, status FROM tasks WHERE id = $1`
	err = tx.QueryRow(query, createdTask.ID).Scan(&dbTask.ID, &dbTask.Title, &dbTask.Description, &dbTask.DueDate, &dbTask.Priority, &dbTask.Status)
	if err != nil {
		t.Fatalf("Failed to fetch created task from DB: %v", err)
	}

	if dbTask.ID != createdTask.ID || dbTask.Title != createdTask.Title {
		t.Errorf("Task in database does not match created task. DB: %+v, Created: %+v", dbTask, createdTask)
	}
}

func TestGetTaskByID(t *testing.T) {

	tx, err := testDB.Begin()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	appWithTx := &App{DB: tx}

	taskToFetch := Task{
		Title:       "Task to Fetch",
		Description: "This task will be fetched by ID",
		DueDate:     "2024-11-20",
		Priority:    3,
		Status:      "Completed",
	}
	createdID := createTaskInDB(t, tx, taskToFetch)

	reqExisting, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("/tasks/%d", createdID), nil)
	rrExisting := executeRequest(reqExisting, appWithTx.getTaskByID)

	if status := rrExisting.Code; status != http.StatusOK {
		t.Errorf("Handler returned wrong status code for existing task: got %v want %v", status, http.StatusOK)
		t.Errorf("Response body: %s", rrExisting.Body.String())
	}

	var fetchedTask Task
	if err := json.Unmarshal(rrExisting.Body.Bytes(), &fetchedTask); err != nil {
		t.Fatalf("Could not unmarshal response body for existing task: %v", err)
	}

	if fetchedTask.ID != createdID || fetchedTask.Title != taskToFetch.Title ||
		fetchedTask.Description != taskToFetch.Description || fetchedTask.DueDate != taskToFetch.DueDate ||
		fetchedTask.Priority != taskToFetch.Priority || fetchedTask.Status != taskToFetch.Status {
		t.Errorf("Fetched task data mismatch. Got %+v, want %+v", fetchedTask, taskToFetch)
	}

	// Test fetching a non-existing task
	nonExistingID := 99999
	reqNonExisting, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("/tasks/%d", nonExistingID), nil)
	rrNonExisting := executeRequest(reqNonExisting, appWithTx.getTaskByID)

	// Check the status code for non-existing task
	if status := rrNonExisting.Code; status != http.StatusNotFound {
		t.Errorf("Handler returned wrong status code for non-existing task: got %v want %v", status, http.StatusNotFound)
	}

	// Check the response body for non-existing task
	expectedErrorMsg := "tarea no encontrada\n"
	if rrNonExisting.Body.String() != expectedErrorMsg {
		t.Errorf("Handler returned unexpected body for non-existing task: got %q want %q", rrNonExisting.Body.String(), expectedErrorMsg)
	}
}

func TestDeleteExistingTask(t *testing.T) {

	tx, err := testDB.Begin()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	appWithTx := &App{DB: tx}

	taskToCreate := Task{
		Title:       "Task to Delete",
		Description: "This task will be deleted",
		DueDate:     "2024-12-31",
		Priority:    2,
		Status:      "Pending",
	}
	createdID := createTaskInDB(t, tx, taskToCreate)

	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("/tasks/%d", createdID), nil)

	rr := executeRequest(req, appWithTx.deleteTask)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	expectedMessage := fmt.Sprintf("Task with ID %d deleted successfully", createdID)
	var response map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Could not unmarshal response body: %v", err)
	}
	if msg, ok := response["message"]; !ok || msg != expectedMessage {
		t.Errorf("Handler returned unexpected body: got %v want {\"message\": \"%s\"}", rr.Body.String(), expectedMessage)
	}

	getReq, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("/tasks/%d", createdID), nil)
	getRR := executeRequest(getReq, appWithTx.getTaskByID)

	if status := getRR.Code; status != http.StatusNotFound {
		t.Errorf("After deletion, GET handler returned wrong status code: got %v want %v", status, http.StatusNotFound)
	}

	expectedErrorMsg := "tarea no encontrada\n"
	if getRR.Body.String() != expectedErrorMsg {
		t.Errorf("After deletion, GET handler returned unexpected body: got %q want %q", getRR.Body.String(), expectedErrorMsg)
	}
}

func TestDeleteNonExistingTask(t *testing.T) {

	tx, err := testDB.Begin()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	appWithTx := &App{DB: tx}

	nonExistingID := 99999

	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("/tasks/%d", nonExistingID), nil)

	rr := executeRequest(req, appWithTx.deleteTask)

	if status := rr.Code; status != http.StatusNotFound {
		t.Errorf("Handler returned wrong status code: got %v want %v", status, http.StatusNotFound)
	}

	expectedErrorMsg := "tarea no encontrada\n"
	if rr.Body.String() != expectedErrorMsg {
		t.Errorf("Handler returned unexpected body: got %q want %q", rr.Body.String(), expectedErrorMsg)
	}
}

func TestDeleteDoesNotAffectOtherTasks(t *testing.T) {

	tx, err := testDB.Begin()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	appWithTx := &App{DB: tx}

	taskA := Task{Title: "Task A", DueDate: "2024-11-01", Priority: 1}
	taskB := Task{Title: "Task B (to delete)", DueDate: "2024-11-02", Priority: 2}
	taskC := Task{Title: "Task C", DueDate: "2024-11-03", Priority: 3}

	idA := createTaskInDB(t, tx, taskA)
	idB := createTaskInDB(t, tx, taskB)
	idC := createTaskInDB(t, tx, taskC)

	deleteReq, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("/tasks/%d", idB), nil)
	deleteRR := executeRequest(deleteReq, appWithTx.deleteTask)

	if status := deleteRR.Code; status != http.StatusOK {
		t.Fatalf("Failed to delete Task B: got status %v, body %q", status, deleteRR.Body.String())
	}

	getAllReq, _ := http.NewRequest(http.MethodGet, "/tasks", nil)
	getAllRR := executeRequest(getAllReq, appWithTx.getAllTasks)

	if status := getAllRR.Code; status != http.StatusOK {
		t.Fatalf("Handler returned wrong status code for GET /tasks: got %v want %v", status, http.StatusOK)
	}

	var remainingTasks []Task
	if err := json.Unmarshal(getAllRR.Body.Bytes(), &remainingTasks); err != nil {
		t.Fatalf("Could not unmarshal response body for GET /tasks: %v", err)
	}

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

	if len(remainingTasks) != 2 {
		t.Errorf("Expected 2 remaining tasks, but found %d", len(remainingTasks))
	}
}

func TestGetAllTasksSortedByPriority(t *testing.T) {
	// Use a transaction for test isolation
	tx, err := testDB.Begin()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback() // Rollback the transaction at the end of the test

	// Create an App instance using the transaction
	appWithTx := &App{DB: tx}

	// Create tasks with different priorities
	taskHigh := Task{Title: "High Priority Task", Description: "Desc H", DueDate: "2024-12-01", Priority: 1, Status: "Pending"}
	taskMedium1 := Task{Title: "Medium Priority Task 1", Description: "Desc M1", DueDate: "2024-12-05", Priority: 2, Status: "Pending"}
	taskLow := Task{Title: "Low Priority Task", Description: "Desc L", DueDate: "2024-12-10", Priority: 3, Status: "Pending"}
	taskMedium2 := Task{Title: "Medium Priority Task 2", Description: "Desc M2", DueDate: "2024-12-06", Priority: 2, Status: "Pending"}

	// Create tasks in the database using the transaction
	createTaskInDB(t, tx, taskLow) // Create in arbitrary order
	createTaskInDB(t, tx, taskHigh)
	createTaskInDB(t, tx, taskMedium2)
	createTaskInDB(t, tx, taskMedium1)

	// Make a GET request to fetch all tasks
	req, _ := http.NewRequest(http.MethodGet, "/tasks", nil)
	rr := executeRequest(req, appWithTx.getAllTasks)

	// Check the status code
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Handler returned wrong status code: got %v want %v", status, http.StatusOK)
		t.Errorf("Response body: %s", rr.Body.String())
	}

	// Unmarshal the response body
	var tasks []Task
	if err := json.Unmarshal(rr.Body.Bytes(), &tasks); err != nil {
		t.Fatalf("Could not unmarshal response body: %v", err)
	}

	// Assert the order of tasks by priority
	// Expected order: High (1), Medium (2), Medium (2), Low (3)
	expectedPriorities := []int{1, 2, 2, 3}

	if len(tasks) != len(expectedPriorities) {
		t.Fatalf("Expected %d tasks, but got %d", len(expectedPriorities), len(tasks))
	}

	for i, task := range tasks {
		if task.Priority != expectedPriorities[i] {
			t.Errorf("Task at index %d has wrong priority: got %v want %v", i, task.Priority, expectedPriorities[i])
		}
	}

	// Optional: Check if tasks with the same priority are sorted by another criteria (e.g., DueDate or Title)
	// The current implementation sorts only by priority. If secondary sorting is needed,
	// the SQL query in main.go would need to be updated (e.g., ORDER BY priority ASC, due_date ASC).
	// For now, we only assert the primary sort by priority.
}

func TestGetAllTasksSortedByDueDate(t *testing.T) {
	// Use a transaction for test isolation
	tx, err := testDB.Begin()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback() // Rollback the transaction at the end of the test

	// Create an App instance using the transaction
	appWithTx := &App{DB: tx}

	// Create tasks with different due dates (out of order)
	taskLater := Task{Title: "Later Due Date", Description: "Desc L", DueDate: "2025-01-01", Priority: 2, Status: "Pending"}
	taskEarlier := Task{Title: "Earlier Due Date", Description: "Desc E", DueDate: "2024-12-01", Priority: 1, Status: "Pending"}
	taskMiddle := Task{Title: "Middle Due Date", Description: "Desc M", DueDate: "2024-12-15", Priority: 3, Status: "Pending"}

	// Create tasks in the database using the transaction
	createTaskInDB(t, tx, taskLater)
	createTaskInDB(t, tx, taskMiddle)
	createTaskInDB(t, tx, taskEarlier)

	// Make a GET request to fetch all tasks, requesting sort by due_date
	req, _ := http.NewRequest(http.MethodGet, "/tasks?sort_by=due_date", nil)
	rr := executeRequest(req, appWithTx.getAllTasks)

	// Check the status code
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Handler returned wrong status code: got %v want %v", status, http.StatusOK)
		t.Errorf("Response body: %s", rr.Body.String())
	}

	// Unmarshal the response body
	var tasks []Task
	if err := json.Unmarshal(rr.Body.Bytes(), &tasks); err != nil {
		t.Fatalf("Could not unmarshal response body: %v", err)
	}

	// Assert the order of tasks by due date (earliest to latest)
	// Expected order: taskEarlier, taskMiddle, taskLater
	expectedTitlesInOrder := []string{"Earlier Due Date", "Middle Due Date", "Later Due Date"}

	if len(tasks) != len(expectedTitlesInOrder) {
		t.Fatalf("Expected %d tasks, but got %d", len(expectedTitlesInOrder), len(tasks))
	}

	for i, task := range tasks {
		if task.Title != expectedTitlesInOrder[i] {
			t.Errorf("Task at index %d has wrong title (due date order): got %q want %q", i, task.Title, expectedTitlesInOrder[i])
		}
	}
}

func TestUpdateTaskPerformance(t *testing.T) {
	// Use a transaction for test isolation
	tx, err := testDB.Begin()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback() // Rollback the transaction at the end of the test

	// Create an App instance using the transaction
	appWithTx := &App{DB: tx}

	// Create a task to be updated
	taskToUpdate := Task{
		Title:       "Task for Performance Test",
		Description: "This task will be updated",
		DueDate:     "2024-12-01",
		Priority:    2,
		Status:      "Pending",
	}
	createdID := createTaskInDB(t, tx, taskToUpdate)

	// Prepare the update data (e.g., change status)
	updateData := map[string]string{"status": "Completed"}
	jsonUpdateData, _ := json.Marshal(updateData)

	// Create the PUT request
	req, _ := http.NewRequest(http.MethodPut, fmt.Sprintf("/tasks/%d", createdID), bytes.NewBuffer(jsonUpdateData))
	req.Header.Set("Content-Type", "application/json")

	// Measure the time taken to execute the update handler
	startTime := time.Now()
	rr := executeRequest(req, appWithTx.updateTask)
	elapsedTime := time.Since(startTime)

	// Define the maximum acceptable duration (1 second)
	maxDuration := 1 * time.Second

	// Check the status code
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Handler returned wrong status code: got %v want %v", status, http.StatusOK)
		t.Errorf("Response body: %s", rr.Body.String())
	}

	// Assert that the update took less than the maximum allowed duration
	if elapsedTime > maxDuration {
		t.Errorf("Task update took too long: got %v, want less than %v", elapsedTime, maxDuration)
	}

	// Optional: Verify the task was actually updated in the DB (already covered by other tests, but good for completeness)
	var updatedTask Task
	query := `SELECT id, title, description, due_date, priority, status FROM tasks WHERE id = $1`
	err = tx.QueryRow(query, createdID).Scan(&updatedTask.ID, &updatedTask.Title, &updatedTask.Description, &updatedTask.DueDate, &updatedTask.Priority, &updatedTask.Status)
	if err != nil {
		t.Fatalf("Failed to fetch updated task from DB: %v", err)
	}

	if updatedTask.Status != "Completed" {
		t.Errorf("Task status was not updated correctly: got %q want %q", updatedTask.Status, "Completed")
	}
}

func TestUpdateTaskSuccessResponse(t *testing.T) {
	// Use a transaction for test isolation
	tx, err := testDB.Begin()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback() // Rollback the transaction at the end of the test

	// Create an App instance using the transaction
	appWithTx := &App{DB: tx}

	// Create a task to be updated
	taskToUpdate := Task{
		Title:       "Original Title",
		Description: "Original Description",
		DueDate:     "2024-12-01",
		Priority:    2,
		Status:      "Pending",
	}
	createdID := createTaskInDB(t, tx, taskToUpdate)

	// Prepare the update data
	updateData := map[string]interface{}{
		"title":       "Updated Title",
		"description": "Updated Description",
		"due_date":    "2025-01-15",
		"priority":    1,
		"status":      "Completed",
	}
	jsonUpdateData, _ := json.Marshal(updateData)

	// Create the PUT request
	req, _ := http.NewRequest(http.MethodPut, fmt.Sprintf("/tasks/%d", createdID), bytes.NewBuffer(jsonUpdateData))
	req.Header.Set("Content-Type", "application/json")

	// Execute the update handler
	rr := executeRequest(req, appWithTx.updateTask)

	// Check the status code
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Handler returned wrong status code: got %v want %v", status, http.StatusOK)
		t.Errorf("Response body: %s", rr.Body.String())
	}

	// Unmarshal the response body into a Task struct
	var updatedTask Task
	if err := json.Unmarshal(rr.Body.Bytes(), &updatedTask); err != nil {
		t.Fatalf("Could not unmarshal response body: %v", err)
	}

	// Assert that the returned task data matches the expected updated values
	if updatedTask.ID != createdID {
		t.Errorf("Returned task ID mismatch: got %v want %v", updatedTask.ID, createdID)
	}
	if updatedTask.Title != updateData["title"] {
		t.Errorf("Returned task title mismatch: got %q want %q", updatedTask.Title, updateData["title"])
	}
	if updatedTask.Description != updateData["description"] {
		t.Errorf("Returned task description mismatch: got %q want %q", updatedTask.Description, updateData["description"])
	}
	if updatedTask.DueDate != updateData["due_date"] {
		t.Errorf("Returned task due date mismatch: got %q want %q", updatedTask.DueDate, updateData["due_date"])
	}
	if updatedTask.Priority != updateData["priority"] {
		t.Errorf("Returned task priority mismatch: got %v want %v", updatedTask.Priority, updateData["priority"])
	}
	if updatedTask.Status != updateData["status"] {
		t.Errorf("Returned task status mismatch: got %q want %q", updatedTask.Status, updateData["status"])
	}

	// Optional: Verify the task was actually updated in the DB
	var dbTask Task
	query := `SELECT id, title, description, due_date, priority, status FROM tasks WHERE id = $1`
	err = tx.QueryRow(query, createdID).Scan(&dbTask.ID, &dbTask.Title, &dbTask.Description, &dbTask.DueDate, &dbTask.Priority, &dbTask.Status)
	if err != nil {
		t.Fatalf("Failed to fetch updated task from DB: %v", err)
	}

	if dbTask.Title != updateData["title"] || dbTask.Status != updateData["status"] {
		t.Errorf("Task in database was not updated correctly. DB: %+v, Expected updates: %+v", dbTask, updateData)
	}
}
