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
	"testing"

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


	nonExistingID := 99999 
	reqNonExisting, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("/tasks/%d", nonExistingID), nil)
	rrNonExisting := executeRequest(reqNonExisting, appWithTx.getTaskByID)

	
	if status := rrNonExisting.Code; status != http.StatusNotFound {
		t.Errorf("Handler returned wrong status code for non-existing task: got %v want %v", status, http.StatusNotFound)
	}

	
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
