package goredisclone

import (
	"errors"
	"fmt"
	"io"
	"log/slog" // Mengimpor slog untuk logging
	"net"      // Mengimpor paket net untuk menangani koneksi jaringan
	"strings"
	"sync"        // Mengimpor paket sync untuk concurrency (Mutex)
	"sync/atomic" // Mengimpor paket atomic untuk operasi atomic
)

// Struktur server yang akan menyimpan state server
type server struct {
	listener net.Listener // Listener yang menerima koneksi dari client
	logger   *slog.Logger // Logger untuk mencatat informasi dan error

	started      atomic.Bool        // Atomic boolean untuk melacak status apakah server sudah dimulai
	clients      map[int64]net.Conn // Map untuk menyimpan koneksi client yang terhubung
	lastClientId int64              // ID terakhir dari client yang terhubung
	clientsLock  sync.Mutex         // Mutex untuk melindungi akses ke map clients
	shuttingDown bool               // Flag untuk menunjukkan apakah server sedang dalam proses shutdown

	dbLock   sync.RWMutex
	database map[string]string
}

// Fungsi untuk membuat server baru dengan listener dan logger
func NewServer(listener net.Listener, logger *slog.Logger) *server {
	return &server{
		listener:     listener,                      // Listener dari parameter
		logger:       logger,                        // Logger dari parameter
		started:      atomic.Bool{},                 // Inisialisasi atomic bool untuk mengecek status start
		clients:      make(map[int64]net.Conn, 100), // Inisialisasi map clients dengan kapasitas 100
		lastClientId: 0,                             // Inisialisasi ID client terakhir sebagai 0
		clientsLock:  sync.Mutex{},                  // Inisialisasi Mutex untuk sinkronisasi akses ke clients
		shuttingDown: false,                         // Inisialisasi flag shuttingDown sebagai false

		dbLock:   sync.RWMutex{},
		database: make(map[string]string),
	}
}

// Fungsi untuk memulai server dan mulai menerima koneksi client
func (s *server) Start() error {
	// Memastikan server hanya bisa dimulai sekali dengan CompareAndSwap
	if !s.started.CompareAndSwap(false, true) {
		return fmt.Errorf("server already started") // Jika sudah dimulai, kembalikan error
	}

	s.logger.Info("server started") // Mencatat bahwa server telah dimulai

	// Loop untuk menerima koneksi client secara terus-menerus
	for {
		conn, err := s.listener.Accept() // Menerima koneksi dari client
		if err != nil {                  // Jika terjadi error saat menerima koneksi
			s.clientsLock.Lock()             // Lock sebelum akses ke shuttingDown
			isShuttingDown := s.shuttingDown // Cek apakah server sedang shutdown
			s.clientsLock.Unlock()           // Unlock setelah pengecekan

			if !isShuttingDown { // Jika server tidak dalam proses shutdown, kembalikan error
				return err
			}

			return nil // Jika server sedang shutdown, keluar dari loop tanpa error
		}

		s.clientsLock.Lock()            // Lock untuk sinkronisasi akses ke clients
		s.lastClientId += 1             // Increment ID client terakhir
		clientId := s.lastClientId      // Simpan ID client saat ini
		s.clients[clientId] = conn      // Tambahkan client ke map clients
		s.clientsLock.Unlock()          // Unlock setelah modifikasi map selesai
		go s.handleConn(clientId, conn) // Jalankan handler untuk client ini di goroutine terpisah
	}
}

// Fungsi untuk menghentikan server dan menutup semua koneksi
func (s *server) Stop() error {
	s.clientsLock.Lock()         // Lock untuk melindungi akses ke clients dan shuttingDown
	defer s.clientsLock.Unlock() // Unlock secara otomatis setelah fungsi ini selesai

	if s.shuttingDown { // Cek apakah server sudah dalam proses shutdown
		return fmt.Errorf("Already shutting down") // Jika sudah, kembalikan error
	}

	s.shuttingDown = true

	// Tutup semua koneksi client yang tersimpan di map clients
	for clientId, conn := range s.clients {
		s.logger.Info(
			"closing client", // Log bahwa client akan ditutup
			slog.Int64("clientId", clientId),
		)

		if err := conn.Close(); err != nil { // Tutup koneksi client
			s.logger.Error(
				"cannot close client", // Log error jika gagal menutup koneksi
				slog.Int64("clientId", clientId),
				slog.String("err", err.Error()),
			)
		}
	}

	clear(s.clients) // Bersihkan semua client dari map clients

	// Tutup listener (tidak menerima koneksi baru)
	if err := s.listener.Close(); err != nil {
		s.logger.Error("cannot stop listener", // Log error jika gagal menutup listener
			slog.String("err", err.Error()),
		)
	}

	return nil // Mengembalikan nil sebagai tanda berhasil
}

// Fungsi yang menangani koneksi dari client
func (s *server) handleConn(clientId int64, conn net.Conn) {
	s.logger.Info( // Log informasi client yang baru terhubung
		"client connected",
		slog.Int64("id", clientId),
		slog.String("host", conn.RemoteAddr().String()),
	)

	for {
		request, err := readArray(conn, true)
		if err != nil { // Jika terjadi error saat membaca data
			if !errors.Is(err, io.EOF) {
				s.logger.Error(
					"error reading from client",
					slog.Int64("clientId", clientId),
					slog.String("err", err.Error()),
				)
			}

			break
		}

		s.logger.Debug(
			"request received",
			slog.Any("request", request),
			slog.Any("clientId", clientId),
		)

		if len(request) == 0 {
			s.logger.Error("missing command in the request", slog.Int64("clientId", clientId))
			break
		}

		commandName, ok := request[0].(string)
		if !ok {
			s.logger.Error("command is not a string", slog.Int64("clientId", clientId))
			break
		}

		switch strings.ToUpper(commandName) {
		case "GET":
			err = s.handleGetCommand(clientId, conn, request)

		case "SET":
			err = s.handleSetCommand(clientId, conn, request)

		default:
			s.logger.Debug("unknown command", slog.String("command", commandName), slog.Int64("clientId", clientId))
			break
		}

		if _, err := conn.Write([]byte("+OK\r\n")); err != nil { // Tulis kembali data ke client (echo)
			s.logger.Error(
				"error writing to client",
				slog.Int64("clientId", clientId),
				slog.String("err", err.Error()),
			)
		}
	}
	// Lock untuk memastikan operasi aman terhadap akses bersamaan ke clients
	s.clientsLock.Lock()
	if _, ok := s.clients[clientId]; !ok { // Cek apakah client masih ada di map clients
		s.clientsLock.Unlock() // Jika tidak, unlock dan keluar
		return
	}

	delete(s.clients, clientId) // Hapus client dari map clients
	s.clientsLock.Unlock()      // Unlock setelah penghapusan selesai

	// Tutup koneksi client dan log jika terjadi error
	s.logger.Info("client disconnecting", slog.Int64("clientId", clientId))
	if err := conn.Close(); err != nil {
		s.logger.Error("cannot close client",
			slog.Int64("clientId", clientId),
			slog.String("err", err.Error()),
		)
	}
}

func (s *server) handleGetCommand(clientId int64, conn net.Conn, command []any) error {
	if len(command) < 2 {
		_, err := conn.Write([]byte("-ERR missing key\r\n"))
		return err
	}

	key, ok := command[1].(string)
	if !ok {
		_, err := conn.Write([]byte("-ERR missing key\r\n"))
		return err
	}

	s.logger.Debug("GET key", slog.String("key", key), slog.Int64("clientId", clientId))

	// Get key here
	s.dbLock.RLock()
	value, ok := s.database[key]
	s.dbLock.RUnlock()

	var err error
	if ok {
		resp := fmt.Sprintf("$%d\r\n%s\r\n", len(value), value)
		_, err = conn.Write([]byte(resp))
	} else {
		_, err = conn.Write([]byte("_\r\n"))
	}

	return err
}

func (s *server) handleSetCommand(clientId int64, conn net.Conn, command []any) error {
	if len(command) < 3 {
		_, err := conn.Write([]byte("-ERR missing ket and value\r\n"))
		return err
	}

	key, ok := command[1].(string)
	if !ok {
		_, err := conn.Write([]byte("-ERR missing ket and value\r\n"))
		return err
	}

	value, ok := command[2].(string)
	if !ok {
		_, err := conn.Write([]byte("-ERR missing ket and value\r\n"))
		return err
	}

	s.logger.Debug(
		"SET key int o value",
		slog.String("key", key),
		slog.String("value", value),
		slog.Int64("clientId", clientId),
	)

	// Lock set here
	s.dbLock.Lock()
	s.database[key] = value
	s.dbLock.Unlock()

	_, err := conn.Write([]byte("+OK\r\n"))
	return err
}
