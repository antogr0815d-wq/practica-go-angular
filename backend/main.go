package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
)

type tarea struct {
	ID         int64  `json:"id"`
	Titulo     string `json:"titulo"`
	Completada bool   `json:"completada"`
}

type api struct {
	db *sql.DB
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := abrirBaseDeDatos(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	auth, err := prepararAutenticacion(ctx, db)
	if err != nil {
		log.Fatal(err)
	}
	cancel()

	tareas := api{db: db}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		responderJSON(w, http.StatusOK, map[string]string{"mensaje": "¡Bienvenido! El servidor está activo."})
	})
	mux.HandleFunc("POST /login", auth.iniciar)
	mux.HandleFunc("GET /sesion", auth.exigirSesion(func(w http.ResponseWriter, _ *http.Request, s sesion) {
		responderJSON(w, http.StatusOK, identidad{Usuario: s.usuario})
	}))
	mux.HandleFunc("POST /logout", auth.cerrar)
	mux.HandleFunc("GET /tareas", auth.proteger(tareas.listar))
	mux.HandleFunc("POST /tareas", auth.proteger(tareas.crear))
	mux.HandleFunc("PUT /tareas", auth.proteger(tareas.actualizar))
	mux.HandleFunc("DELETE /tareas", auth.proteger(tareas.eliminar))

	servidor := &http.Server{
		Addr:              "0.0.0.0:8080",
		Handler:           conCORS(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       time.Minute,
	}
	log.Printf("Servidor HTTP escuchando en %s", servidor.Addr)
	log.Fatal(servidor.ListenAndServe())
}

func abrirBaseDeDatos(ctx context.Context) (*sql.DB, error) {
	config := mysql.NewConfig()
	config.User = "root"
	config.Net = "tcp"
	config.Addr = os.Getenv("MYSQL_ADDR")
	if config.Addr == "" {
		config.Addr = "127.0.0.1:3306"
	}
	config.Timeout = 5 * time.Second
	config.ReadTimeout = 5 * time.Second
	config.WriteTimeout = 5 * time.Second
	config.ClientFoundRows = true

	servidor, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("configurar conexión a MySQL: %w", err)
	}
	defer servidor.Close()
	if _, err := servidor.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS practica_db CHARACTER SET utf8mb4"); err != nil {
		return nil, fmt.Errorf("crear practica_db en MySQL (%s): %w", config.Addr, err)
	}

	config.DBName = "practica_db"
	db, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("abrir practica_db: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(3 * time.Minute)
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS tareas (
		id INT AUTO_INCREMENT PRIMARY KEY,
		titulo VARCHAR(255) NOT NULL,
		completada BOOLEAN DEFAULT FALSE
	) DEFAULT CHARACTER SET utf8mb4`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("crear tabla tareas: %w", err)
	}
	return db, nil
}

func (a api) listar(w http.ResponseWriter, r *http.Request) {
	filas, err := a.db.QueryContext(r.Context(), `SELECT id, titulo, COALESCE(completada, FALSE)
		FROM tareas ORDER BY id`)
	if err != nil {
		responderErrorBD(w, err)
		return
	}
	defer filas.Close()

	tareas := []tarea{}
	for filas.Next() {
		var t tarea
		if err := filas.Scan(&t.ID, &t.Titulo, &t.Completada); err != nil {
			responderErrorBD(w, err)
			return
		}
		tareas = append(tareas, t)
	}
	if err := filas.Err(); err != nil {
		responderErrorBD(w, err)
		return
	}
	responderJSON(w, http.StatusOK, tareas)
}

func (a api) crear(w http.ResponseWriter, r *http.Request) {
	t, ok := leerTarea(w, r)
	if !ok {
		return
	}
	resultado, err := a.db.ExecContext(r.Context(), "INSERT INTO tareas (titulo, completada) VALUES (?, ?)", t.Titulo, t.Completada)
	if err != nil {
		responderErrorBD(w, err)
		return
	}
	t.ID, err = resultado.LastInsertId()
	if err != nil {
		responderErrorBD(w, err)
		return
	}
	responderJSON(w, http.StatusCreated, t)
}

func (a api) actualizar(w http.ResponseWriter, r *http.Request) {
	id, ok := leerID(w, r)
	if !ok {
		return
	}
	t, ok := leerTarea(w, r)
	if !ok {
		return
	}
	t.ID = id
	resultado, err := a.db.ExecContext(r.Context(), "UPDATE tareas SET titulo = ?, completada = ? WHERE id = ?", t.Titulo, t.Completada, id)
	if tareaEncontrada(w, resultado, err) {
		responderJSON(w, http.StatusOK, t)
	}
}

func (a api) eliminar(w http.ResponseWriter, r *http.Request) {
	id, ok := leerID(w, r)
	if !ok {
		return
	}
	resultado, err := a.db.ExecContext(r.Context(), "DELETE FROM tareas WHERE id = ?", id)
	if tareaEncontrada(w, resultado, err) {
		w.WriteHeader(http.StatusNoContent)
	}
}

func tareaEncontrada(w http.ResponseWriter, resultado sql.Result, err error) bool {
	if err != nil {
		responderErrorBD(w, err)
		return false
	}
	filas, err := resultado.RowsAffected()
	if err != nil {
		responderErrorBD(w, err)
		return false
	}
	if filas == 0 {
		responderError(w, http.StatusNotFound, "La tarea no existe.")
		return false
	}
	return true
}

func leerID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 32)
	if err != nil || id < 1 {
		responderError(w, http.StatusBadRequest, "Indica un id válido en la URL, por ejemplo: /tareas?id=1.")
		return 0, false
	}
	return id, true
}

func leerTarea(w http.ResponseWriter, r *http.Request) (tarea, bool) {
	var t tarea
	if !leerJSON(w, r, &t) {
		responderError(w, http.StatusBadRequest, "El cuerpo debe contener una tarea en JSON.")
		return tarea{}, false
	}
	t.Titulo = strings.TrimSpace(t.Titulo)
	if t.Titulo == "" || utf8.RuneCountInString(t.Titulo) > 255 {
		responderError(w, http.StatusBadRequest, "El título debe contener entre 1 y 255 caracteres.")
		return tarea{}, false
	}
	return t, true
}

func leerJSON(w http.ResponseWriter, r *http.Request, destino any) bool {
	cuerpo := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024))
	return cuerpo.Decode(destino) == nil && cuerpo.Decode(new(any)) == io.EOF
}

func responderErrorBD(w http.ResponseWriter, err error) {
	log.Printf("Error de MySQL: %v", err)
	responderError(w, http.StatusInternalServerError, "Error al consultar la base de datos.")
}

func responderError(w http.ResponseWriter, estado int, mensaje string) {
	responderJSON(w, estado, map[string]string{"error": mensaje})
}

func responderJSON(w http.ResponseWriter, estado int, valor any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(estado)
	if err := json.NewEncoder(w).Encode(valor); err != nil {
		log.Printf("Error al enviar la respuesta JSON: %v", err)
	}
}

func conCORS(siguiente http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		siguiente.ServeHTTP(w, r)
	})
}
