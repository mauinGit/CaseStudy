# Catatan Studi Kasus — Back-End GDGoC UNSRI

Bahan untuk `BE_CaseStudy_Maulana Adiatma.PDF`. Setiap bagian ditulis sebagai poin yang bisa langsung disusun ulang menjadi narasi.

- **Repo:** https://github.com/mauinGit/CaseStudy
- **Live API:** belum tersedia (deploy terkendala verifikasi kartu kredit, lihat bagian 8). Jalankan lokal: `docker compose up --build`, Swagger UI di http://localhost:8080/docs
- **Stack:** Go + Gin, PostgreSQL 16 (pgx, tanpa ORM), golang-migrate, JWT + bcrypt, Google OAuth, Docker, GitHub Actions, Render + Neon

---

## 1. Analisis masalah

**Kebutuhan inti.** Sistem untuk mengelola Core Team GDGoC UNSRI: autentikasi, tiga role (Lead, Division Head, Member) dengan hak akses berbeda, manajemen anggota, event dengan kuota, pendaftaran, check-in kehadiran, dan audit log.

**Tantangan yang paling menentukan desain:**

1. **Konsistensi saat konkurensi.** Kuota tidak boleh terlampaui walau banyak orang mendaftar di detik yang sama; satu orang tidak boleh terdaftar dua kali; check-in yang dikirim berulang tidak boleh membuat data ganda.
2. **Otorisasi dua dimensi.** Hak akses tidak cukup dilihat dari role saja; harus digabung dengan *kepemilikan data* (divisi, diri sendiri). Tanpa ini muncul IDOR, misalnya Division Head mengubah anggota divisi lain hanya dengan mengganti ID di URL.
3. **Jejak audit yang dapat dipercaya.** Perubahan sensitif (role, hapus anggota, kuota) harus selalu punya jejak dan jejak itu tidak boleh bisa diubah.
4. **Keamanan autentikasi.** Brute force, enumerasi akun, token akun yang sudah dihapus, dan mass assignment (`"role": "lead"` di body).
5. **Deploy gratis yang tetap andal.** Cold start Render, batas koneksi Neon, dan IP klien di balik load balancer.

**Asumsi yang diambil** (karena tidak dijelaskan di brief):

- Akun dibuat oleh Lead/Division Head (tidak ada self sign-up). Login Google hanya untuk email yang sudah terdaftar dan berdomain yang diizinkan.
- Semua anggota boleh melihat dan mendaftar ke event divisi mana pun; yang dibatasi adalah *pengelolaan* event dan *data peserta*.
- Event umum (tanpa divisi) hanya dikelola Lead. Division Head hanya mengelola anggota ber-role `member` di divisinya.
- Check-in dilakukan oleh peserta sendiri, dan hanya saat event berlangsung.

---

## 2. Solusi dan cara kerja

### Arsitektur

- Berlapis: **handler** (bind + validasi DTO) → **service** (otorisasi + aturan bisnis + transaksi) → **repository** (SQL) → **PostgreSQL**.
- Error bisnis didefinisikan di `domain` dengan kode stabil (`QUOTA_FULL`, `ALREADY_REGISTERED`, …) dan dipetakan ke status HTTP di satu tempat (`apperror`). Format respons selalu konsisten: `{"data": ...}` atau `{"error": {"code", "message", "fields?"}}`.
- Migrasi SQL di-embed ke binary dan dijalankan otomatis saat start; seed idempotent (ID tetap + `ON CONFLICT DO NOTHING` + advisory lock).

### Model data (pertahanan berlapis di database)

- `users` — `UNIQUE(email)` + `CHECK(email = lower(email))`, non-Lead wajib punya divisi (`CHECK`), soft delete via `deleted_at`.
- `events` — `CHECK(registered_count <= quota)`, `CHECK(end_at > start_at)`, `CHECK(quota > 0)`.
- `event_registrations` — `UNIQUE(event_id, user_id)`, status `registered`/`cancelled`.
- `attendances` — `UNIQUE(event_id, user_id)` + FK komposit ke `event_registrations` (check-in mustahil tanpa pendaftaran).
- `audit_logs` — JSONB `old_value`/`new_value`, IP, actor; trigger menolak `UPDATE`/`DELETE` (append-only).
- Index parsial `WHERE deleted_at IS NULL` dan index trigram (`pg_trgm`) untuk pencarian `ILIKE`.

### Alur penting

**Daftar event** (satu transaksi):
1. `UPDATE events SET registered_count = registered_count + 1 WHERE id=$1 AND registered_count < quota AND deleted_at IS NULL AND end_at > now() RETURNING id` → 0 baris = `QUOTA_FULL` / `EVENT_ENDED`.
2. `INSERT INTO event_registrations ... ON CONFLICT ... DO UPDATE SET status='registered' WHERE status='cancelled'` → tidak ada baris = sudah terdaftar aktif → `ALREADY_REGISTERED` → **rollback** (slot dari langkah 1 ikut kembali).

**Batal daftar** (satu transaksi): kunci baris event, lalu baris pendaftaran → tolak jika event selesai atau sudah check-in → ubah status `cancelled` → `registered_count - 1`.

**Check-in** (satu transaksi): jika sudah ada attendance → kembalikan (200). Jika belum: cek `start_at <= now <= end_at`, kunci pendaftaran `FOR SHARE` dan pastikan aktif → `INSERT ... ON CONFLICT DO NOTHING RETURNING` → jika kalah balapan, baca baris pemenang (200), jika menang (201).

**Ubah role / hapus anggota** (satu transaksi): kunci baris pelaku dan target (`FOR UPDATE`, urut id) → cek ulang role pelaku dari baris terkunci → cek hak atas target → ubah → tulis audit log.

---

## 3. Alasan desain (trade-off)

| Keputusan | Alternatif | Alasan memilih |
|---|---|---|
| `UPDATE ... WHERE registered_count < quota` (atomik) | `SELECT` lalu cek di aplikasi; `SELECT ... FOR UPDATE`; isolasi `SERIALIZABLE`; counter di Redis | Satu statement, lock baris sangat singkat, tidak perlu retry, tidak ada celah "cek lalu tulis". `SERIALIZABLE` butuh retry loop; Redis menambah komponen dan masalah konsistensi dengan DB. |
| `registered_count` disimpan (denormalisasi) | `COUNT(*)` setiap pendaftaran | Cek kuota O(1) dan bisa dijaga `CHECK` constraint. Konsistensinya dijaga karena hanya diubah di transaksi yang sama dengan baris pendaftaran (diuji: counter = jumlah baris). |
| Role & divisi dibaca ulang dari DB setiap request | Percaya claim di JWT | Biaya 1 query per request (pakai primary key), imbalannya: perubahan role dan penghapusan akun berlaku seketika tanpa daftar blokir token. |
| Data di luar jangkauan → 404, terlihat tapi tak boleh diubah → 403 | Selalu 403 | 404 tidak membocorkan keberadaan data (mencegah enumerasi ID). |
| Audit log di transaksi yang sama + trigger append-only | Log asinkron / tabel biasa | Tidak mungkin ada perubahan tanpa jejak atau jejak tanpa perubahan; jejak tidak bisa diedit lewat aplikasi. |
| SQL manual (pgx), tanpa ORM | GORM dsb. | Kontrol penuh atas query atomik, `FOR UPDATE`/`FOR SHARE`, `ON CONFLICT`, dan nama constraint untuk pemetaan error. |
| Soft delete | Hard delete | Riwayat event dan audit tetap utuh; akun yang dihapus tidak bisa login dan tokennya langsung ditolak. |
| Satu DTO per endpoint | Bind langsung ke model | Mass assignment mustahil: field yang tidak dideklarasikan tidak pernah dibaca. |

---

## 4. Desain autentikasi dan otorisasi

### Autentikasi

- **Password:** bcrypt cost 12 (minimal 10, maks 72 byte). Email salah, password salah, akun terhapus, dan akun khusus Google mengembalikan respons **identik** (`INVALID_CREDENTIALS`); user yang tidak ada tetap dibandingkan dengan *dummy hash* agar waktu respons setara (anti enumerasi).
- **Rate limit login:** 5/menit per IP+email (anti brute force satu akun) dan 30/menit per IP (anti password spraying) → 429 + `Retry-After`.
- **JWT:** HS256 dengan algoritma dikunci (token `alg: none` / algoritma lain ditolak), klaim `iss`, `exp`, `nbf`, `iat`, `jti`; secret ≥ 32 karakter dari env; TTL 2 jam. Token hanya berisi ID user; role selalu diambil dari DB.
- **Google OAuth:** authorization code + **PKCE** + `state` acak di cookie HttpOnly/SameSite (anti CSRF, dibandingkan constant-time). Email harus `email_verified`, berdomain `ALLOWED_EMAIL_DOMAIN` (dan klaim `hd` cocok), dan sudah terdaftar sebagai anggota. Akun Google ditautkan via `sub` saat login pertama.

### Otorisasi

- Dicek di **service layer** (bukan hanya router), menggabungkan **role** dan **kepemilikan data**:

| Aksi | Lead | Division Head | Member |
|---|---|---|---|
| Lihat anggota | semua | divisinya | diri sendiri |
| Kelola anggota | semua (kecuali hapus diri sendiri) | `member` di divisinya | — |
| Ubah role | ✓ (kecuali diri sendiri), diaudit | — | — |
| Kelola event | semua + event umum | event divisinya | — |
| Daftar / batal / check-in | ✓ | ✓ | ✓ |
| Lihat peserta & kehadiran | semua | semua di event divisinya, anggota divisinya di event lain | data sendiri |
| Audit log | ✓ | — | — |

- **Filter tidak bisa memperluas cakupan:** cakupan list dipaksa dari role (Member selalu `user_id = dirinya`; Division Head selalu divisinya — meminta divisi lain → 403).
- **Tahan race:** untuk mutasi anggota, baris pelaku dan target dikunci lalu role pelaku dibaca ulang di dalam transaksi. Dua Lead yang saling menurunkan role bersamaan → tepat satu berhasil, organisasi tidak pernah tanpa Lead. Lead juga tidak bisa menurunkan/menghapus dirinya sendiri.

---

## 5. Mitigasi keamanan (dipetakan ke OWASP API Security Top 10)

| Risiko | Mitigasi |
|---|---|
| API1 Broken Object Level Authorization (IDOR) | Cek kepemilikan di service; data di luar jangkauan → 404; diuji per role. |
| API2 Broken Authentication | bcrypt, rate limit, respons login identik, JWT dikunci algoritmanya, token akun terhapus langsung ditolak, PKCE + state untuk OAuth. |
| API3 Broken Object Property Level Authorization (mass assignment) | DTO per endpoint; `role`/`division_id`/`deleted_at`/`password_hash` diabaikan; UPDATE repository hanya menulis kolom aman; respons tidak pernah memuat `password_hash`/`google_sub`. |
| API4 Unrestricted Resource Consumption | `limit` maks 100, body maks 1 MiB, rate limit login, timeout server HTTP. |
| API5 Broken Function Level Authorization | Ubah role & audit log hanya Lead; kelola event hanya Lead/Division Head; dicek di service. |
| API8 Security Misconfiguration | Security headers (CSP ketat, `nosniff`, `X-Frame-Options`, HSTS di produksi, `no-store`), CORS whitelist, mode release, recovery tanpa stack trace, container distroless non-root. |
| Injection | Semua query parameterized; kolom sort dari whitelist; wildcard `%`/`_` di-escape. |
| Secret | Hanya dari env; `.env` di-gitignore; `.env.example`; JWT secret dibuat Render; gitleaks di CI dan riwayat git sudah dipindai (0 temuan). |
| Spoofing IP | Hanya proxy privat yang dipercaya (`TRUSTED_PROXIES`), jadi `X-Forwarded-For` palsu dari klien tidak memengaruhi rate limit/audit. |

---

## 6. Kualitas dan pengujian

- **40 fungsi test** (±1.900 baris) dijalankan terhadap **PostgreSQL sungguhan**; setiap test punya database terisolasi yang dibuat dan dihapus otomatis.
- Test wajib dari brief:
  - Kuota: **50 goroutine vs kuota 10 → tepat 10 berhasil** (3 ronde), counter = jumlah baris pendaftaran.
  - Pendaftaran ganda: berurutan dan 20 request bersamaan → tepat 1.
  - Role: Member tidak bisa ubah role / buat event; Division Head tidak bisa mengelola divisi lain; IDOR.
  - Mass assignment: `role: "lead"` di `PATCH /me` → role tetap (dicek langsung ke DB).
  - Check-in: 2x dan 20x bersamaan → 1 baris; di luar jendela waktu dan non-peserta ditolak.
- **Mutation check:** test konkurensi terbukti menangkap bug — menghapus `registered_count < quota`, menghapus `ON CONFLICT`, atau menghapus cek ulang role di dalam transaksi membuat test gagal. Dari sini ditemukan juga bahwa pool koneksi yang lazy membuat "request bersamaan" sebenarnya berurutan; test sekarang membuka koneksi lebih dulu agar benar-benar paralel.
- **CI (GitHub Actions)** setiap push & PR: gofmt, vet, build, cek spesifikasi OpenAPI sesuai kode, `go test -race` dengan PostgreSQL 16, `docker build`, gitleaks.
- **Git:** commit atomik Conventional Commits, 3 PR (`feat/auth-and-members`, `feat/events-attendance`, `ci/tests-and-deploy`), setiap commit dicek bisa di-build sendiri.

---

## 7. Skalabilitas dan future-proofing

**Yang sudah siap:**

- API **stateless** (JWT) → bisa diskalakan horizontal di balik load balancer.
- Semua list memakai pagination dengan batas, index sesuai pola query (parsial + trigram), dan query yang mengunci baris sesingkat mungkin.
- Versi API di path (`/api/v1`), migrasi berversi, konfigurasi lewat env, spesifikasi OpenAPI untuk klien, dan arsitektur berlapis sehingga aturan bisnis bisa diubah tanpa menyentuh HTTP/SQL.
- Container kecil (distroless), health check, graceful shutdown.

**Yang perlu diubah saat skala naik:** lihat jawaban singkat nomor 2.

---

## 8. Deploy

- **Status: belum di-deploy ke publik.** Render meminta verifikasi kartu kredit/debit (otorisasi $1) dan kartu ditolak; alternatif juga tidak bisa dipakai (Koyeb tidak menerima pendaftaran baru, trial Railway sudah habis, Docker di Hugging Face Spaces berbayar). Aplikasi tetap bisa dinilai penuh secara lokal dengan `docker compose up --build`, dan siap deploy tanpa perubahan kode begitu kartu tersedia.
- **Rencana deploy: Render** (Docker, free, Singapore) + **Neon** PostgreSQL (Singapore) agar latensi API ↔ DB rendah. Konfigurasi di `render.yaml`; auto-deploy dari `main`; health check `/healthz` (cek koneksi DB).
- Migrasi dan seed berjalan otomatis saat start. Koneksi memakai endpoint **direct** Neon (bukan pooler) karena golang-migrate memakai advisory lock level session; `DB_MAX_CONNS=5` sesuai batas free tier.
- **Keep-alive:** cron-job.org memanggil `/healthz` setiap 10 menit agar Render tidak tidur. Request pertama setelah idle tetap bisa lambat (±30–60 detik).

---

## 9. Jawaban singkat

### (1) Bagaimana kuota dijaga saat dua request berebut slot terakhir?

Pengecekan dan penambahan kuota dilakukan dalam **satu statement atomik** di dalam transaksi:

```sql
UPDATE events SET registered_count = registered_count + 1
WHERE id = $1 AND registered_count < quota AND deleted_at IS NULL AND end_at > now()
RETURNING id;
```

Misal kuota 10 dan sudah terisi 9. Request A dan B datang bersamaan. A mengunci baris event dan menaikkan counter menjadi 10. B harus **menunggu lock** baris itu. Setelah A commit, PostgreSQL **mengevaluasi ulang** kondisi B terhadap nilai terbaru (`10 < 10` salah), sehingga `UPDATE` milik B tidak mengubah baris apa pun → `QUOTA_FULL`. Tidak ada pola "baca, cek di aplikasi, lalu tulis" yang bisa disalip. Jika A ternyata pendaftar ganda, `INSERT` pendaftaran gagal → seluruh transaksi A di-rollback → slot kembali dan B (atau request berikutnya) bisa mendapatkannya. Sebagai pertahanan terakhir, `CHECK (registered_count <= quota)` membuat database menolak nilai melebihi kuota. Dibuktikan dengan test 50 goroutine vs kuota 10.

### (2) Perubahan desain untuk ribuan anggota dan event

- **Pagination:** ganti offset ke **keyset/cursor pagination** (`WHERE (created_at, id) < ($1, $2)`) agar halaman dalam tetap cepat.
- **Rate limit & state bersama:** pindahkan rate limiter dari memori ke **Redis** agar berlaku konsisten di banyak instance.
- **Database:** connection pooler (PgBouncer/Neon pooler) untuk banyak instance, **read replica** untuk endpoint baca berat (list event, peserta), dan **partisi `audit_logs` per bulan** + kebijakan retensi.
- **Event sangat populer (hot row):** semua pendaftaran ke satu event mengantre di satu baris. Untuk ribuan request/detik: antrean pendaftaran (worker memproses berurutan dan memberi status), **waitlist**, atau sharded counter.
- **Caching:** cache list/detail event (berubah jarang) dengan invalidasi saat event diubah.
- **Pekerjaan asinkron:** notifikasi/email lewat job queue, bukan di request.
- **Observability:** metrics (latensi, error rate, lock wait), tracing, dan alert.
- **Authorization:** jika aturan makin kompleks (role per divisi, multi-chapter), pindahkan kebijakan ke policy engine terpusat.

### (3) Satu kelemahan saat ini dan rencana perbaikannya

**Kelemahan:** rate limiter login disimpan **di memori proses**. Akibatnya:
- Jika API dijalankan lebih dari satu instance, batas 5 percobaan/menit berlaku **per instance**, sehingga penyerang yang request-nya tersebar ke beberapa instance mendapat kuota percobaan berlipat.
- Hitungan hilang setiap kali service restart/deploy (di Render free, juga setiap bangun dari tidur).

**Rencana perbaikan:**
1. Pindahkan penyimpanan ke **Redis** (mis. Upstash/Redis Cloud) dengan algoritma **sliding window** atomik (`INCR` + `EXPIRE` atau skrip Lua) dengan kunci yang sama (`ip|email` dan `ip`).
2. Tambahkan **penguncian bertahap per akun** (mis. jeda eksponensial setelah N kegagalan) yang disimpan di database, agar tetap efektif walau IP berganti-ganti.
3. Middleware hanya memanggil satu method, `Allow(key)`. Cukup ekstrak interface `Limiter` dari implementasi memori yang sekarang, lalu suntikkan implementasi Redis lewat router tanpa mengubah handler; tambahkan test integrasi dengan Redis di CI.

*Kelemahan lain yang juga tercatat:* email anggota yang di-soft delete tetap terpakai (rencana: unique index parsial `WHERE deleted_at IS NULL` + endpoint restore) dan belum ada refresh token / logout sisi server (rencana: refresh token berotasi yang bisa dicabut).
