package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	_ "github.com/lib/pq"
)

// Define an interface for database operations used by App methods
type DBExecutor interface {
	Query(query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(query string, args ...interface{}) *sql.Row
	Exec(query string, args ...interface{}) (sql.Result, error)
}

type Task struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	DueDate     string `json:"due_date"`
	Priority    int    `json:"priority"`
	Status      string `json:"status"`
}

type App struct {
	DB DBExecutor
}

func (app *App) createTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var task Task
	if err := json.NewDecoder(r.Body).Decode(&task); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if task.ID <= 0 {
		http.Error(w, "Invalid task data: a positive integer ID is required", http.StatusBadRequest)
		return
	}

	if task.Title == "" || task.DueDate == "" || task.Priority < 1 || task.Priority > 3 {
		http.Error(w, "Invalid task data: title, due_date, and priority (1-3) are required", http.StatusBadRequest)
		return
	}

	if task.Status == "" {
		task.Status = "Pending"
	}

	query := `INSERT INTO tasks (id, title, description, due_date, priority, status)
              VALUES ($1, $2, $3, $4, $5, $6)`
	_, err := app.DB.Exec(query, task.ID, task.Title, task.Description, task.DueDate, task.Priority, task.Status)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key value violates unique constraint") {
			http.Error(w, fmt.Sprintf("Task with ID %d already exists", task.ID), http.StatusConflict)
			return
		}
		log.Printf("Error creating task: %v", err)
		http.Error(w, "Failed to create task", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(task)
}

func (app *App) getAllTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rows, err := app.DB.Query("SELECT id, title, description, due_date, priority, status FROM tasks ORDER BY priority ASC")
	if err != nil {
		log.Printf("Error fetching tasks: %v", err)
		http.Error(w, "Failed to fetch tasks", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.Title, &t.Description, &t.DueDate, &t.Priority, &t.Status); err != nil {
			log.Printf("Error scanning task: %v", err)
			http.Error(w, "Failed to fetch tasks", http.StatusInternalServerError)
			return
		}
		tasks = append(tasks, t)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks)
}

func (app *App) getTaskByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/tasks/")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid task ID", http.StatusBadRequest)
		return
	}

	var task Task
	query := `SELECT id, title, description, due_date, priority, status FROM tasks WHERE id = $1`
	err = app.DB.QueryRow(query, id).Scan(&task.ID, &task.Title, &task.Description, &task.DueDate, &task.Priority, &task.Status)
	if err == sql.ErrNoRows {
		http.Error(w, "tarea no encontrada", http.StatusNotFound)
		return
	} else if err != nil {
		log.Printf("Error fetching task: %v", err)
		http.Error(w, "Failed to fetch task", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(task)
}

func (app *App) deleteTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/tasks/")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid task ID", http.StatusBadRequest)
		return
	}

	query := `DELETE FROM tasks WHERE id = $1 RETURNING id`
	var deletedID int
	err = app.DB.QueryRow(query, id).Scan(&deletedID)

	if err == sql.ErrNoRows {
		http.Error(w, "tarea no encontrada", http.StatusNotFound)
		return
	} else if err != nil {
		log.Printf("Error deleting task: %v", err)
		http.Error(w, "Failed to delete task", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": fmt.Sprintf("Task with ID %d deleted successfully", deletedID)})
}

func (app *App) updateTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/tasks/")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid task ID", http.StatusBadRequest)
		return
	}

	var taskUpdates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&taskUpdates); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Build the update query dynamically based on provided fields
	var setClauses []string
	var args []interface{}
	argIndex := 1

	if status, ok := taskUpdates["status"].(string); ok && status != "" {
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", argIndex))
		args = append(args, status)
		argIndex++
	}

	// Add other fields if needed in the future (e.g., title, description, etc.)
	// if title, ok := taskUpdates["title"].(string); ok && title != "" {
	// 	setClauses = append(setClauses, fmt.Sprintf("title = $%d", argIndex))
	// 	args = append(args, title)
	// 	argIndex++
	// }
	// ... add other fields ...

	if len(setClauses) == 0 {
		http.Error(w, "No fields to update provided", http.StatusBadRequest)
		return
	}

	query := fmt.Sprintf("UPDATE tasks SET %s WHERE id = $%d RETURNING id", strings.Join(setClauses, ", "), argIndex)
	args = append(args, id)

	var updatedID int
	err = app.DB.QueryRow(query, args...).Scan(&updatedID)

	if err == sql.ErrNoRows {
		http.Error(w, "tarea no encontrada", http.StatusNotFound)
		return
	} else if err != nil {
		log.Printf("Error updating task: %v", err)
		http.Error(w, "Failed to update task", http.StatusInternalServerError)
		return
	}

	// Optionally fetch and return the updated task
	var updatedTask Task
	fetchQuery := `SELECT id, title, description, due_date, priority, status FROM tasks WHERE id = $1`
	err = app.DB.QueryRow(fetchQuery, updatedID).Scan(&updatedTask.ID, &updatedTask.Title, &updatedTask.Description, &updatedTask.DueDate, &updatedTask.Priority, &updatedTask.Status)
	if err != nil {
		log.Printf("Error fetching updated task: %v", err)
		// Still return OK, but maybe log the fetch error
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"message": fmt.Sprintf("Task with ID %d updated successfully (could not fetch updated details)", updatedID)})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(updatedTask)
}

func main() {
	connStr := "postgresql://calidadsoftware_user:Pp5IT3eGOO0fPHRx6ubNtUOuEu55zK7q@dpg-d0clc0be5dus73agl78g-a.oregon-postgres.render.com/calidadsoftware"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("Error connecting to database:", err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS tasks (
		id INT PRIMARY KEY,
		title TEXT NOT NULL,
		description TEXT,
		due_date TEXT NOT NULL,
		priority INTEGER NOT NULL CHECK (priority >= 1 AND priority <= 3),
		status TEXT NOT NULL DEFAULT 'Pending'
	)`)
	if err != nil {
		log.Fatal("Error creating table:", err)
	}

	app := &App{DB: db}

	http.HandleFunc("/tasks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			app.createTask(w, r)
		} else if r.Method == http.MethodGet {
			app.getAllTasks(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})
	http.HandleFunc("/tasks/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			app.getTaskByID(w, r)
		} else if r.Method == http.MethodDelete {
			app.deleteTask(w, r)
		} else if r.Method == http.MethodPut {
			app.updateTask(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			return
		}
		http.DefaultServeMux.ServeHTTP(w, r)
	})

	log.Println("Server running on :8080")
	if err := http.ListenAndServe(":8080", handler); err != nil {
		log.Fatal("Server failed:", err)
	}
}
