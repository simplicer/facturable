// Command selloctl: CLI interno del proyecto.
//
//	selloctl claves [--dir etc]   genera el par Ed25519 del servicio de licencias
package main

import (
	"flag"
	"fmt"
	"os"

	"facturable/internal/licenses"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("uso: selloctl <claves>")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "claves":
		fs := flag.NewFlagSet("claves", flag.ExitOnError)
		dir := fs.String("dir", "etc", "directorio de configuración")
		_ = fs.Parse(os.Args[2:])
		priv, pub, err := licenses.LoadOrCreateKey(*dir + "/licenses.ed25519")
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		fmt.Printf("clave privada: %s/licenses.ed25519\nclave pública:  %s/licenses.ed25519.pub (%x)\n", *dir, *dir, pub)
		_ = priv
	default:
		fmt.Println("subcomando desconocido:", os.Args[1])
		os.Exit(2)
	}
}
