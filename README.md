# GDGoC UNSRI — Core Team Management API

[![CI](https://github.com/mauinGit/CaseStudy/actions/workflows/ci.yml/badge.svg)](https://github.com/mauinGit/CaseStudy/actions/workflows/ci.yml)

REST API untuk mengelola Core Team dan anggota GDGoC UNSRI: autentikasi (email + password dan Google OAuth), otorisasi berbasis role (Lead, Division Head, Member), manajemen anggota, event dengan kuota, pendaftaran, kehadiran (check-in), dan audit log.

Dibangun dengan **Go + Gin**, **PostgreSQL 16** (pgx, tanpa ORM), migrasi **golang-migrate**, JWT + bcrypt, OpenAPI/Swagger, Docker, dan GitHub Actions.

| | |
|---|---|
| Live API | Belum tersedia: deploy terkendala verifikasi kartu kredit ([detail](#deploy-render--neon)). Jalankan lokal dengan `docker compose up --build`. |
| Swagger UI | `/docs` (mis. http://localhost:8080/docs) |
| Spesifikasi OpenAPI | [`openapi.yaml`](openapi.yaml) (juga di `/docs/doc.json`) |
| Health check | `GET /healthz` |

---

## Daftar isi

- [Menjalankan dengan satu perintah](#menjalankan-dengan-satu-perintah)
- [Akun uji](#akun-uji)
- [Contoh alur pemakaian](#contoh-alur-pemakaian)
- [Endpoint](#endpoint)
- [Matriks hak akses (RBAC)](#matriks-hak-akses-rbac)
- [Arsitektur](#arsitektur)
- [Keputusan desain penting](#keputusan-desain-penting)
- [Keamanan](#keamanan)
- [Format respons dan kode error](#format-respons-dan-kode-error)
- [Testing](#testing)
- [CI](#ci)
- [Konfigurasi](#konfigurasi)
- [Deploy (Render + Neon)](#deploy-render--neon)
- [Keterbatasan dan rencana](#keterbatasan-dan-rencana)

---

## Menjalankan dengan satu perintah

Prasyarat: Docker (dengan Docker Compose).

```bash
docker compose up --build
```

Perintah ini menjalankan PostgreSQL 16 lalu API. Saat startup, API otomatis menjalankan **migrasi** (`migrations/`, di-embed ke binary) dan **seed idempotent** (`seed/`), jadi aman dijalankan berulang kali.

- API: http://localhost:8080
- Swagger UI: http://localhost:8080/docs
- Health check: http://localhost:8080/healthz

Untuk menghapus data lokal dan memulai dari nol: `docker compose down -v`.

### Tanpa Docker

Butuh Go (lihat versi di `go.mod`) dan PostgreSQL sendiri.

```bash
cp .env.example .env        # lalu isi nilainya (jangan di-commit)
go run ./cmd/api            # jalankan server
go run ./cmd/api migrate    # hanya migrasi lalu keluar
go run ./cmd/api seed       # migrasi + seed lalu keluar
```

`Makefile` menyediakan pintasan: `make up`, `make down`, `make test`, `make lint`, `make docs`.

---

## Akun uji

Seed membuat 4 divisi (Back-End, Front-End, Mobile, UI/UX), 8 akun, dan 6 event. Semua akun memakai password yang sama dari `SEED_PASSWORD`. Di Docker Compose lokal defaultnya **`GdgocUnsri2026!`**. Untuk deployment produksi, password diatur lewat `SEED_PASSWORD` di Render dan tidak dipublikasikan di repo.

| Role | Email | Divisi |
|---|---|---|
| Lead | `lead@gdgoc-unsri.test` | — |
| Division Head | `head.backend@gdgoc-unsri.test` | Back-End |
| Division Head | `head.frontend@gdgoc-unsri.test` | Front-End |
| Member | `member.backend1@gdgoc-unsri.test` | Back-End |
| Member | `member.backend2@gdgoc-unsri.test` | Back-End |
| Member | `member.frontend@gdgoc-unsri.test` | Front-End |
| Member | `member.mobile@gdgoc-unsri.test` | Mobile |
| Member | `member.uiux@gdgoc-unsri.test` | UI/UX |

Event seed (waktu relatif terhadap saat seed dijalankan):

| Event | Divisi | Status | Kuota | Kegunaan |
|---|---|---|---|---|
| Back-End Bootcamp: Go REST API | Back-End | berlangsung (60 hari) | 30 | uji check-in |
| Study Jam: Google Cloud Fundamentals | umum | berlangsung (60 hari) | 100 | uji check-in |
| Mini Hackathon Front-End | Front-End | +7 hari | 2 (1 terisi) | uji `QUOTA_FULL` |
| Tech Talk: Career in Tech | umum | +14 hari | 50 | uji `CHECK_IN_NOT_OPEN` |
| Kickoff Core Team 2026 | umum | sudah selesai | 80 | uji `EVENT_ENDED` |
| UI/UX Design Sprint | UI/UX | +10 hari | 1 (penuh) | uji `QUOTA_FULL` |

---

## Contoh alur pemakaian

```bash
API=http://localhost:8080/api/v1

# 1. Login -> access token (JWT, berlaku 2 jam)
TOKEN=$(curl -s -X POST $API/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"member.backend2@gdgoc-unsri.test","password":"GdgocUnsri2026!"}' | jq -r .data.access_token)

# 2. Event yang sedang berlangsung
curl -s "$API/events?status=ongoing" -H "Authorization: Bearer $TOKEN" | jq

# 3. Daftar ke event, lalu check-in (check-in kedua mengembalikan data yang sama, status 200)
EVENT=30000000-0000-4000-8000-000000000001
curl -s -X POST $API/events/$EVENT/register -H "Authorization: Bearer $TOKEN" | jq
curl -s -X POST $API/events/$EVENT/check-in -H "Authorization: Bearer $TOKEN" | jq
```

Di Swagger UI: jalankan `POST /api/v1/auth/login`, salin `access_token`, klik **Authorize**, lalu isi `Bearer <token>`.

---

## Endpoint

Semua endpoint di bawah `/api/v1` kecuali `/healthz` dan `/docs`. 🔒 = butuh `Authorization: Bearer <token>`.

| Method | Path | Keterangan |
|---|---|---|
| GET | `/healthz` | Status API dan database (200 / 503) |
| POST | `/auth/login` | Login email + password (rate limited) |
| GET | `/auth/google/login` | Mulai login Google (redirect) |
| GET | `/auth/google/callback` | Callback Google OAuth |
| GET 🔒 | `/me` | Profil sendiri |
| PATCH 🔒 | `/me` | Ubah profil sendiri (hanya `name`, `phone`, `bio`) |
| GET 🔒 | `/divisions` | Daftar divisi |
| GET 🔒 | `/members` | Daftar anggota: `page`, `limit` (≤100), `role`, `division_id`, `search`, `sort` (`name`/`created_at`/`email`), `order` |
| POST 🔒 | `/members` | Tambah anggota (registrasi oleh Lead / Division Head) |
| GET 🔒 | `/members/{id}` | Detail anggota |
| PATCH 🔒 | `/members/{id}` | Ubah data anggota (`name`, `email`, `phone`, `bio`) |
| DELETE 🔒 | `/members/{id}` | Soft delete anggota |
| PATCH 🔒 | `/members/{id}/role` | Ubah role (dan divisi) anggota, hanya Lead, diaudit |
| GET 🔒 | `/events` | Daftar event: `page`, `limit`, `division_id`, `status` (`upcoming`/`ongoing`/`finished`), `search`, `sort`, `order` |
| POST 🔒 | `/events` | Buat event |
| GET 🔒 | `/events/{id}` | Detail event (termasuk `remaining_quota`, `status`) |
| PATCH 🔒 | `/events/{id}` | Ubah event (perubahan kuota diaudit) |
| DELETE 🔒 | `/events/{id}` | Soft delete event |
| POST 🔒 | `/events/{id}/register` | Daftar ke event |
| DELETE 🔒 | `/events/{id}/register` | Batalkan pendaftaran (slot dikembalikan) |
| GET 🔒 | `/events/{id}/participants` | Daftar peserta (`status=registered|cancelled`) |
| POST 🔒 | `/events/{id}/check-in` | Check-in (idempotent: 201 pertama kali, 200 berikutnya) |
| GET 🔒 | `/events/{id}/attendances` | Daftar kehadiran |
| GET 🔒 | `/audit-logs` | Audit log, hanya Lead: `page`, `limit`, `action`, `entity`, `entity_id`, `actor_id` |

Detail request/response ada di Swagger UI dan [`openapi.yaml`](openapi.yaml).

---

## Matriks hak akses (RBAC)

| Aksi | Lead | Division Head | Member |
|---|---|---|---|
| Lihat daftar anggota | semua | divisinya sendiri | dirinya sendiri |
| Tambah / ubah / soft delete anggota | semua (kecuali hapus diri sendiri) | anggota ber-role `member` di divisinya | — |
| Ubah profil sendiri (`/me`) | ✓ | ✓ | ✓ |
| Ubah role anggota | ✓ (kecuali role sendiri), diaudit | — | — |
| Buat / ubah / hapus event | semua event, termasuk event umum | event divisinya | — |
| Lihat event, daftar, batal, check-in | ✓ | ✓ | ✓ |
| Lihat peserta / kehadiran | semua | semua di event divisinya; di event lain hanya anggota divisinya | data dirinya sendiri |
| Lihat audit log | ✓ | — | — |

Otorisasi selalu memeriksa **role dan kepemilikan data**, dan dijalankan di **service layer**, bukan hanya di router.

---

## Arsitektur

```
cmd/api/              entrypoint: serve | migrate | seed, graceful shutdown
internal/config/      konfigurasi dari env (tanpa secret hardcode), divalidasi saat start
internal/http/        router dan middleware (auth, rate limit, security headers, CORS, body limit, recovery)
internal/http/handler request DTO, validasi, mapping response (satu DTO per endpoint)
internal/service/     aturan bisnis dan otorisasi (role + kepemilikan data)
internal/repository/  semua SQL (parameterized), transaksi (Store.InTx), row locking
internal/domain/      entity dan error domain (tanpa ketergantungan HTTP/DB)
internal/apperror/    mapping error domain -> status HTTP + format JSON yang konsisten
migrations/           skema database (SQL, di-embed ke binary)
seed/                 data awal dan akun uji (idempotent)
docs/, openapi.yaml   spesifikasi OpenAPI hasil generate (swag)
```

Alur request: **middleware → handler** (bind + validasi DTO) **→ service** (otorisasi + aturan bisnis, transaksi) **→ repository** (SQL) **→ PostgreSQL**.

### Skema database

| Tabel | Isi | Constraint penting |
|---|---|---|
| `divisions` | divisi | `UNIQUE(name)` |
| `users` | anggota (`role`: `lead`/`division_head`/`member`) | `UNIQUE(email)`, email wajib lowercase (`CHECK`), non-Lead wajib punya divisi (`CHECK`), `deleted_at` untuk soft delete |
| `events` | event, `quota`, `registered_count` | `CHECK (registered_count <= quota)`, `CHECK (end_at > start_at)`, `CHECK (quota > 0)` |
| `event_registrations` | pendaftaran (`registered`/`cancelled`) | `UNIQUE(event_id, user_id)` |
| `attendances` | check-in | `UNIQUE(event_id, user_id)`, FK komposit ke `event_registrations` |
| `audit_logs` | jejak audit (JSONB `old_value`/`new_value`, IP) | append-only: trigger menolak `UPDATE`/`DELETE` |

Index parsial (`WHERE deleted_at IS NULL`) untuk filter yang sering dipakai, dan index trigram (`pg_trgm`) supaya pencarian `ILIKE '%kata%'` pada nama/email tetap memakai index.

---

## Keputusan desain penting

### 1. Kuota tidak pernah terlampaui, walau banyak request bersamaan

Pendaftaran berjalan dalam **satu transaksi**:

```sql
-- (1) ambil slot: kondisi dan increment dalam SATU statement
UPDATE events SET registered_count = registered_count + 1
WHERE id = $1 AND registered_count < quota AND deleted_at IS NULL AND end_at > now()
RETURNING id;            -- 0 baris => QUOTA_FULL (atau EVENT_ENDED)

-- (2) simpan pendaftaran, dijaga UNIQUE(event_id, user_id)
INSERT INTO event_registrations ... ON CONFLICT ... DO UPDATE ... WHERE status = 'cancelled';
```

PostgreSQL mengunci baris event saat `UPDATE`. Request lain yang berebut slot terakhir menunggu, lalu **mengevaluasi ulang** `registered_count < quota` terhadap nilai terbaru setelah request pertama commit. Hasilnya hanya satu yang berhasil. Tidak ada pola "baca dulu, cek di aplikasi, lalu tulis" yang rawan race condition. `CHECK (registered_count <= quota)` di database menjadi pertahanan terakhir.

Test: 50 goroutine berebut kuota 10, diulang 3 ronde, hasilnya selalu tepat 10 berhasil dan 40 `QUOTA_FULL`.

### 2. Pendaftaran ganda → rollback

Kalau langkah (2) menemukan pendaftaran aktif (`UNIQUE(event_id, user_id)`), service mengembalikan `ALREADY_REGISTERED` dan **seluruh transaksi di-rollback**, termasuk slot yang sudah diambil di langkah (1). Pendaftaran yang pernah dibatalkan diaktifkan kembali (tidak membuat baris baru). Membatalkan pendaftaran mengurangi `registered_count` dalam transaksi yang sama.

### 3. Check-in idempotent

`INSERT INTO attendances ... ON CONFLICT DO NOTHING RETURNING ...`. Kalau tidak ada baris yang dikembalikan, baris yang sudah ada dibaca dan dikembalikan dengan **200** (check-in pertama **201**). Check-in hanya diterima jika `start_at <= now() <= end_at` dan pengirim punya pendaftaran aktif. Baris pendaftaran dikunci `FOR SHARE`, sehingga check-in dan pembatalan yang bersamaan tidak bisa saling menyalip. Test: 20 check-in bersamaan menghasilkan 1 baris.

### 4. Audit log dalam transaksi yang sama

Perubahan role, soft delete anggota, dan perubahan kuota event ditulis ke `audit_logs` **di transaksi yang sama** dengan perubahannya. Jadi tidak mungkin ada perubahan tanpa jejak, atau jejak tanpa perubahan (diuji dengan memaksa insert audit gagal: perubahan ikut di-rollback). Tabel dilindungi trigger agar append-only.

### 5. Otorisasi yang tahan race condition

- Role dan divisi pengguna **dibaca ulang dari database di setiap request**, bukan dipercaya dari isi JWT. Perubahan role langsung berlaku, dan token akun yang di-soft delete langsung ditolak.
- Untuk ubah/hapus anggota dan ubah role, baris pelaku dan target **dikunci (`FOR UPDATE`, urut id agar bebas deadlock)** lalu role pelaku dicek ulang di dalam transaksi. Dua Lead yang saling menurunkan role secara bersamaan tidak bisa membuat organisasi tanpa Lead (diuji).
- Data di luar jangkauan pengguna dijawab **404** (keberadaannya tidak bocor). Data yang terlihat tetapi tidak boleh diubah dijawab **403**.

### 6. Mass assignment

Setiap endpoint punya DTO sendiri yang hanya mendeklarasikan field yang boleh diubah. `role`, `division_id`, `deleted_at`, `password_hash`, `registered_count`, dan sebagainya di body diabaikan. Role hanya bisa diubah lewat `PATCH /members/{id}/role` (Lead saja). Query `UPDATE` di repository juga hanya menulis kolom yang aman.

### 7. Soft delete anggota

Akun yang dihapus tidak bisa login dan tokennya langsung tidak berlaku. Pendaftaran ke event yang belum selesai otomatis dibatalkan dan slotnya dikembalikan. Riwayat event yang sudah selesai tetap disimpan.

---

## Keamanan

| Risiko | Mitigasi |
|---|---|
| SQL injection | Semua query parameterized (pgx). Kolom sort hanya dari whitelist di server. Wildcard `%`/`_` pada pencarian di-escape. |
| IDOR | Kepemilikan data dicek di service. Data di luar jangkauan dijawab 404. |
| Mass assignment | DTO terpisah per endpoint; kolom sensitif tidak bisa ditulis lewat endpoint profil. |
| Brute force login | Rate limit 5/menit per IP+email dan 30/menit per IP (anti password spraying) → 429 + `Retry-After`. |
| Enumerasi akun | Email salah, password salah, akun terhapus, dan akun khusus Google memberi respons identik. Dummy bcrypt agar waktu respons setara. |
| Penyimpanan password | bcrypt (cost 12, minimal 10), batas 72 byte. |
| JWT | HS256 dengan algoritma dikunci (token `alg: none` ditolak), `iss`/`exp` wajib, secret ≥ 32 karakter dari env. |
| Google OAuth | Authorization code + PKCE + state di cookie HttpOnly (anti CSRF), email harus terverifikasi dan berdomain `ALLOWED_EMAIL_DOMAIN`, hanya untuk anggota yang sudah terdaftar. |
| Kebocoran error | Recovery middleware; error internal hanya ke log, klien menerima pesan generik tanpa stack trace / SQL. |
| Header | `nosniff`, `X-Frame-Options: DENY`, CSP ketat, `Referrer-Policy`, HSTS (produksi), `Cache-Control: no-store` untuk API. |
| CORS | Hanya origin dari `CORS_ALLOWED_ORIGINS`. |
| Payload besar | Body dibatasi 1 MiB (413). |
| Secret | Hanya dari environment. `.env` di-gitignore, `.env.example` disediakan, gitleaks berjalan di CI. |
| Container | Image distroless, non-root, tanpa shell. |

---

## Format respons dan kode error

```jsonc
// sukses
{ "data": { ... } }
// list
{ "data": [ ... ], "meta": { "page": 1, "limit": 20, "total": 8, "total_pages": 1 } }
// error
{ "error": { "code": "QUOTA_FULL", "message": "Kuota event sudah penuh" } }
// error validasi
{ "error": { "code": "VALIDATION_ERROR", "message": "Input tidak valid",
             "fields": [ { "field": "email", "message": "format email tidak valid" } ] } }
```

| HTTP | Kode |
|---|---|
| 400 | `VALIDATION_ERROR`, `DIVISION_REQUIRED`, `INVALID_TIME_RANGE`, `PASSWORD_TOO_LONG`, `INVALID_OAUTH_STATE` |
| 401 | `UNAUTHORIZED`, `INVALID_TOKEN`, `INVALID_CREDENTIALS`, `OAUTH_FAILED` |
| 403 | `FORBIDDEN`, `NOT_REGISTERED`, `CANNOT_CHANGE_OWN_ROLE`, `CANNOT_DELETE_SELF`, `EMAIL_NOT_VERIFIED`, `EMAIL_DOMAIN_NOT_ALLOWED`, `ACCOUNT_NOT_REGISTERED` |
| 404 | `NOT_FOUND`, `ROUTE_NOT_FOUND` |
| 409 | `QUOTA_FULL`, `ALREADY_REGISTERED`, `EVENT_ENDED`, `CHECK_IN_NOT_OPEN`, `ALREADY_CHECKED_IN`, `QUOTA_BELOW_REGISTERED`, `EMAIL_TAKEN` |
| 413 | `PAYLOAD_TOO_LARGE` |
| 422 | `DIVISION_NOT_FOUND` |
| 429 | `TOO_MANY_REQUESTS` |
| 500 / 503 | `INTERNAL_ERROR` / `GOOGLE_OAUTH_DISABLED` |

---

## Testing

Test integrasi berjalan terhadap **PostgreSQL sungguhan**. Setiap test membuat database terisolasi sendiri (`gdgoc_test_<acak>`), menjalankan migrasi dan seed, lalu menghapusnya setelah selesai.

```bash
docker compose up -d db
TEST_DATABASE_URL="postgres://gdgoc:gdgoc@localhost:5432/gdgoc?sslmode=disable" go test -race ./...
```

Tanpa `TEST_DATABASE_URL`, test integrasi di-skip secara lokal, tetapi **gagal** di CI.

Yang diuji, antara lain:

- **Kuota:** 50 goroutine vs kuota 10 → tepat 10 berhasil, `registered_count` selalu sama dengan jumlah baris pendaftaran.
- **Pendaftaran ganda:** berurutan dan 20 request bersamaan → tepat 1, slot request gagal dikembalikan.
- **Batal:** slot kembali, daftar ulang setelah batal, batal setelah check-in / setelah event selesai ditolak.
- **Otorisasi:** Member tidak bisa ubah role atau buat event, Division Head tidak bisa mengelola divisi lain, IDOR pada detail anggota, dan dua Lead saling menurunkan role secara bersamaan.
- **Mass assignment:** `role: "lead"`, `division_id`, `deleted_at`, `password_hash` di `PATCH /me` diabaikan (dicek langsung ke database).
- **Check-in:** 2x dan 20x bersamaan → 1 baris; sebelum mulai, setelah selesai, non-peserta, dan pendaftaran batal ditolak.
- **Audit log:** isi entri, transaksi atomik, append-only, hanya Lead.
- **Auth:** rate limit, token kedaluwarsa / dimanipulasi / `alg: none`, Google OAuth (provider palsu).

Test konkurensi membuka koneksi database lebih dulu sebelum mengirim request bersamaan, agar request benar-benar berjalan paralel. Ini diverifikasi dengan *mutation check*: kalau kondisi kuota atau `ON CONFLICT` dihapus, test-nya gagal.

---

## CI

GitHub Actions ([`.github/workflows/ci.yml`](.github/workflows/ci.yml)) berjalan pada setiap push dan pull request:

1. `gofmt`, `go vet`, `go build`
2. Cek `openapi.yaml` / `docs/` sesuai dengan anotasi kode
3. `go test -race` terhadap service container PostgreSQL 16
4. `docker build`
5. Pemindaian secret dengan gitleaks

---

## Konfigurasi

Semua konfigurasi dari environment variable (lihat [`.env.example`](.env.example)). Konfigurasi divalidasi saat start; aplikasi menolak jalan jika nilai wajib kosong atau lemah.

| Variabel | Default | Keterangan |
|---|---|---|
| `APP_ENV` | `development` | `production` mengaktifkan HSTS, cookie `Secure`, mode release Gin, log JSON |
| `PORT` | `8080` | |
| `LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |
| `DATABASE_URL` | — (wajib) | URL PostgreSQL |
| `DB_MAX_CONNS` | `10` | Ukuran pool koneksi |
| `AUTO_MIGRATE` / `SEED_ON_START` | `false` | Migrasi / seed otomatis saat start |
| `SEED_PASSWORD` | — | Password akun uji (minimal 8 karakter, wajib jika seed aktif) |
| `JWT_SECRET` | — (wajib) | Minimal 32 karakter |
| `JWT_TTL` | `2h` | Masa berlaku access token |
| `BCRYPT_COST` | `12` | 10–31 |
| `LOGIN_RATE_LIMIT` / `LOGIN_IP_RATE_LIMIT` / `LOGIN_RATE_WINDOW` | `5` / `30` / `1m` | Rate limit login |
| `TRUSTED_PROXIES` | kosong | IP/CIDR proxy yang dipercaya untuk `X-Forwarded-For` |
| `CORS_ALLOWED_ORIGINS` | kosong | Origin yang diizinkan, pisahkan dengan koma |
| `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` / `GOOGLE_REDIRECT_URL` | kosong | Google OAuth (opsional; nonaktif jika kosong) |
| `ALLOWED_EMAIL_DOMAIN` | — | Domain email yang boleh login via Google |

---

## Deploy (Render + Neon)

> **Status: belum di-deploy ke publik (kendala kartu kredit).**
> Saat mencoba deploy, semua platform hosting yang dicoba meminta verifikasi kartu kredit/debit atau tidak bisa dipakai:
>
> | Platform | Kendala |
> |---|---|
> | Render | Verifikasi kartu (otorisasi $1) diminta, baik lewat Blueprint maupun Web Service biasa, dan kartu ditolak |
> | Koyeb | Tidak menerima pendaftaran baru (layanan bergabung dengan Mistral) |
> | Railway | Masa trial sudah habis |
> | Hugging Face Spaces (Docker) | Runtime Docker memerlukan paket berbayar |
>
> Aplikasi sudah **siap deploy**: image Docker production (distroless, non-root), konfigurasi [`render.yaml`](render.yaml), migrasi dan seed otomatis saat start, health check `/healthz`, dan semua konfigurasi lewat environment variable. Begitu kartu tersedia, langkah di bawah bisa langsung dijalankan tanpa perubahan kode. Untuk penilaian, aplikasi bisa dijalankan lengkap secara lokal dengan satu perintah: `docker compose up --build` (lihat [Menjalankan dengan satu perintah](#menjalankan-dengan-satu-perintah)).

Rencana deploy memakai **Render** (Web Service, Docker, free plan, region Singapore) dan **Neon** PostgreSQL (region Singapore, agar latensi API ↔ database rendah). Konfigurasi service ada di [`render.yaml`](render.yaml) (Render Blueprint). Tidak ada secret di repo: nilai rahasia diisi di dashboard Render.

### 1. Database (Neon)

1. Buat project di [neon.tech](https://neon.tech) dengan region **AWS Asia Pacific (Singapore)**.
2. Salin connection string **direct** (host *tanpa* `-pooler`), format `postgres://USER:PASSWORD@HOST/DB?sslmode=require`. Endpoint direct dipakai karena golang-migrate memegang advisory lock level session, yang tidak bisa ditahan oleh pooler mode transaksi.

### 2. Web service (Render)

1. Di Render: **New → Blueprint**, pilih repo ini. Render membaca `render.yaml` dan membuat service `gdgoc-core-team-api`.
2. Isi nilai yang diminta:
   - `DATABASE_URL`: connection string Neon dari langkah 1.
   - `SEED_PASSWORD`: password akun uji (minimal 8 karakter).
   - `JWT_SECRET` dibuat otomatis oleh Render.
3. Deploy. Saat start, API menjalankan migrasi dan seed idempotent, lalu Render memeriksa `GET /healthz`.

Setiap push ke `main` memicu deploy ulang otomatis.

| Pengaturan | Nilai | Alasan |
|---|---|---|
| `APP_ENV` | `production` | HSTS, cookie `Secure`, mode release, log JSON |
| `TRUSTED_PROXIES` | rentang IP privat | Load balancer Render menambahkan IP klien asli ke `X-Forwarded-For`. Hanya proxy privat yang dipercaya, sehingga rate limit dan audit log memakai IP asli dan header palsu dari klien diabaikan. |
| `DB_MAX_CONNS` | `5` | Batas koneksi Neon free tier |

Google OAuth tetap nonaktif sampai `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, dan `GOOGLE_REDIRECT_URL` (`https://<service>.onrender.com/api/v1/auth/google/callback`) ditambahkan di dashboard.

### Keep-alive (cron-job.org)

Render free tier menidurkan service setelah ±15 menit tanpa trafik. Supaya API tetap responsif:

1. Buat akun di [cron-job.org](https://cron-job.org).
2. Buat cron job baru dengan URL `https://<render-service>.onrender.com/healthz`, method `GET`, jadwal **setiap 10 menit**.
3. Simpan dan aktifkan.

> **Catatan:** jika service sempat tidur, **request pertama bisa lambat (±30–60 detik)** karena container perlu dinyalakan ulang. Request berikutnya normal. Neon juga menidurkan compute database setelah beberapa menit idle, jadi query pertama setelah idle bisa tertunda ±1 detik.

---

## Keterbatasan dan rencana

- **Rate limit per instance.** Rate limiter login tersimpan di memori, cukup untuk satu instance. Untuk scale-out ke beberapa instance, pindahkan ke Redis agar batasnya berlaku global.
- **Email anggota yang di-soft delete tetap terpakai** (`UNIQUE(email)` mencakup baris terhapus). Rencana: unique index parsial `WHERE deleted_at IS NULL` plus endpoint restore.
- **Tidak ada refresh token / logout server-side.** Token berumur pendek (2 jam) dan langsung tidak berlaku jika akun dihapus. Rencana: refresh token yang dirotasi dan dapat dicabut.
- **Hot row pada event populer.** Semua pendaftaran ke satu event mengantre pada satu baris. Aman dan cukup cepat untuk skala organisasi kampus. Untuk ribuan request per detik, gunakan antrean atau pembagian slot (sharded counter).
