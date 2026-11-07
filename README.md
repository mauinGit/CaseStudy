# GDGoC UNSRI — Core Team Management API

REST API untuk sistem manajemen Core Team dan anggota GDGoC UNSRI
(Go + Gin + PostgreSQL). Dokumentasi lengkap menyusul.

## Menjalankan lokal

```bash
docker compose up --build
```

Perintah di atas menjalankan PostgreSQL 16 dan API. Saat startup API otomatis
menjalankan migrasi (`migrations/`) dan seed idempotent (`seed/`).

- API: http://localhost:8080
- Health check: http://localhost:8080/healthz

Tanpa Docker (butuh PostgreSQL sendiri):

```bash
cp .env.example .env   # lalu sesuaikan nilainya
go run ./cmd/api           # server
go run ./cmd/api migrate   # hanya migrasi
go run ./cmd/api seed      # migrasi + seed
```

## Struktur

```
cmd/api/            entrypoint (serve | migrate | seed)
internal/config/    konfigurasi dari env (tanpa secret hardcode)
internal/database/  pool pgx + migrasi (golang-migrate, file SQL di-embed)
internal/http/      router, middleware, handler
internal/service/   aturan bisnis & otorisasi
internal/repository/ query SQL & transaksi
internal/domain/    entity & error domain
internal/apperror/  mapping error -> HTTP + format JSON konsisten
migrations/         skema database (SQL)
seed/               data awal & akun uji
```
