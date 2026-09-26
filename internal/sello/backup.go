package sello

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"os"

	"crypto/sha256"

	"golang.org/x/crypto/pbkdf2"
)

// backupMagic heads every backup file.
var backupMagic = []byte("FBK1")

// CreateBackup cifra (AES-256-GCM, clave PBKDF2-SHA256 de la contraseña) una
// copia de la base de datos del libro. La copia se toma de un fichero
// físicamente distinto: SQLite en WAL puede estar a mitad de checkpoint, así
// que el destino recomendado es la herramienta sqlite de backup (véase
// Makefile); aquí ciframos lo que el llamador entrega.
func CreateBackup(rutaDB, rutaDestino, password string) error {
	plain, err := os.ReadFile(rutaDB)
	if err != nil {
		return fmt.Errorf("no se pudo leer la base de datos: %v", err)
	}
	out, err := os.Create(rutaDestino)
	if err != nil {
		return fmt.Errorf("no se pudo crear la copia: %v", err)
	}
	defer out.Close()

	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	key := pbkdf2.Key([]byte(password), salt, 210_000, 32, sha256.New)
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	cifrado := gcm.Seal(nil, nonce, plain, nil)

	if _, err := out.Write(backupMagic); err != nil {
		return err
	}
	if _, err := out.Write(salt); err != nil {
		return err
	}
	if _, err := out.Write(nonce); err != nil {
		return err
	}
	_, err = out.Write(cifrado)
	return err
}

// RestoreBackup descifra una copia a un fichero destino.
func RestoreBackup(rutaBackup, rutaDestino, password string) error {
	raw, err := os.ReadFile(rutaBackup)
	if err != nil {
		return fmt.Errorf("no se pudo leer la copia: %v", err)
	}
	if len(raw) < len(backupMagic) || string(raw[:4]) != string(backupMagic) {
		return fmt.Errorf("el fichero no es una copia de seguridad de Facturable")
	}
	body := raw[4:]
	if len(body) < 16+12 {
		return fmt.Errorf("copia truncada")
	}
	salt, rest := body[:16], body[16:]
	nonce, cifrado := rest[:12], rest[12:]

	key := pbkdf2.Key([]byte(password), salt, 210_000, 32, sha256.New)
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	plain, err := gcm.Open(nil, nonce, cifrado, nil)
	if err != nil {
		return fmt.Errorf("contraseña incorrecta o copia dañada: %v", err)
	}
	return os.WriteFile(rutaDestino, plain, 0o600)
}
