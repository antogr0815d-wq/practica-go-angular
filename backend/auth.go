package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	nombreCookie   = "practica_sesion"
	duracionSesion = 8 * time.Hour
)

type identidad struct {
	Usuario string `json:"usuario"`
}

type sesion struct {
	usuario string
	vence   time.Time
}

type autenticacion struct {
	db         *sql.DB
	hashPrueba []byte
	mu         sync.Mutex
	sesiones   map[string]sesion
}

func prepararAutenticacion(ctx context.Context, db *sql.DB) (*autenticacion, error) {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS usuarios (
		id INT AUTO_INCREMENT PRIMARY KEY,
		usuario VARCHAR(100) NOT NULL UNIQUE,
		password VARCHAR(255) NOT NULL
	) DEFAULT CHARACTER SET utf8mb4`); err != nil {
		return nil, fmt.Errorf("crear tabla usuarios: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("12345"), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("generar hash del administrador: %w", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO usuarios (usuario, password) VALUES (?, ?)
		ON DUPLICATE KEY UPDATE id = id`, "admin", hash); err != nil {
		return nil, fmt.Errorf("crear administrador de práctica: %w", err)
	}

	return &autenticacion{db: db, hashPrueba: hash, sesiones: make(map[string]sesion)}, nil
}

func (a *autenticacion) iniciar(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var datos struct {
		Usuario  string `json:"usuario"`
		Password string `json:"password"`
	}
	if !leerJSON(w, r, &datos) {
		responderError(w, http.StatusBadRequest, "Envía el usuario y la contraseña en JSON.")
		return
	}
	usuario := strings.TrimSpace(datos.Usuario)
	if usuario == "" || datos.Password == "" || len(datos.Password) > 72 {
		responderError(w, http.StatusUnauthorized, "Usuario o contraseña incorrectos.")
		return
	}

	var cuenta identidad
	var hash []byte
	err := a.db.QueryRowContext(r.Context(), "SELECT usuario, password FROM usuarios WHERE usuario = ?", usuario).
		Scan(&cuenta.Usuario, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		hash = a.hashPrueba
	} else if err != nil {
		responderErrorBD(w, err)
		return
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(datos.Password)) != nil || err != nil {
		responderError(w, http.StatusUnauthorized, "Usuario o contraseña incorrectos.")
		return
	}

	var aleatorio [32]byte
	if _, err := rand.Read(aleatorio[:]); err != nil {
		log.Printf("Error al generar token de sesión: %v", err)
		responderError(w, http.StatusInternalServerError, "No se pudo crear la sesión.")
		return
	}
	token := base64.RawURLEncoding.EncodeToString(aleatorio[:])
	ahora := time.Now()
	a.mu.Lock()
	for token, s := range a.sesiones {
		if !ahora.Before(s.vence) {
			delete(a.sesiones, token)
		}
	}
	delete(a.sesiones, tokenActual(r))
	a.sesiones[token] = sesion{usuario: cuenta.Usuario, vence: ahora.Add(duracionSesion)}
	a.mu.Unlock()

	http.SetCookie(w, cookieSesion(r, token, int(duracionSesion.Seconds())))
	responderJSON(w, http.StatusOK, cuenta)
}

func (a *autenticacion) cerrar(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	delete(a.sesiones, tokenActual(r))
	a.mu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	http.SetCookie(w, cookieSesion(r, "", -1))
	w.WriteHeader(http.StatusNoContent)
}

func (a *autenticacion) exigirSesion(siguiente func(http.ResponseWriter, *http.Request, sesion)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		token := tokenActual(r)
		a.mu.Lock()
		s, existe := a.sesiones[token]
		vigente := existe && time.Now().Before(s.vence)
		if existe && !vigente {
			delete(a.sesiones, token)
		}
		a.mu.Unlock()
		if !vigente {
			responderError(w, http.StatusUnauthorized, "Inicia sesión para continuar.")
			return
		}
		siguiente(w, r, s)
	}
}

func (a *autenticacion) proteger(siguiente http.HandlerFunc) http.HandlerFunc {
	return a.exigirSesion(func(w http.ResponseWriter, r *http.Request, _ sesion) {
		siguiente(w, r)
	})
}

func tokenActual(r *http.Request) string {
	cookie, err := r.Cookie(nombreCookie)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func cookieSesion(r *http.Request, token string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     nombreCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	}
}
