// Command selloctl: internal project CLI.
//
//	selloctl keys  [-dir etc]                generate the Ed25519 license keypair
//	selloctl sign  -key K -in FILE           write FILE.sig (detached ed25519)
//	selloctl verify -pub P -in FILE [-sig S] verify a signed file
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"facturable/internal/licenses"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "keys":
		fs := flag.NewFlagSet("keys", flag.ExitOnError)
		dir := fs.String("dir", "etc", "config directory")
		out := fs.String("out", "", "optional path for a separate release keypair")
		_ = fs.Parse(os.Args[2:])
		target := *dir + "/licencias.ed25519"
		if *out != "" {
			target = *out
		}
		priv, pub, err := licenses.LoadOrCreateKey(target)
		if err != nil {
			die("keys:", err)
		}
		fmt.Printf("private key: %s\npublic key:  %s\npublic b64:  %s\n",
			target, target+".pub", base64.StdEncoding.EncodeToString(pub))
		_ = priv
	case "sign":
		fs := flag.NewFlagSet("sign", flag.ExitOnError)
		key := fs.String("key", "dist/release.key", "ed25519 private key file")
		in := fs.String("in", "", "file to sign")
		_ = fs.Parse(os.Args[2:])
		if *in == "" {
			die("sign: -in required", nil)
		}
		priv, err := readPriv(*key)
		if err != nil {
			die("sign:", err)
		}
		raw, err := os.ReadFile(*in)
		if err != nil {
			die("sign:", err)
		}
		sig := ed25519.Sign(priv, raw)
		if err := os.WriteFile(*in+".sig", []byte(base64.StdEncoding.EncodeToString(sig)), 0o644); err != nil {
			die("sign:", err)
		}
		fmt.Println("signed:", *in+".sig")
	case "verify":
		fs := flag.NewFlagSet("verify", flag.ExitOnError)
		pub := fs.String("pub", "", "ed25519 public key file or base64")
		in := fs.String("in", "", "signed file")
		sig := fs.String("sig", "", "signature file (default FILE.sig)")
		_ = fs.Parse(os.Args[2:])
		if *in == "" || *pub == "" {
			die("verify: -pub and -in required", nil)
		}
		if *sig == "" {
			*sig = *in + ".sig"
		}
		pubB, err := os.ReadFile(*pub)
		if err != nil {
			// maybe base64 inline
			if p, err2 := base64.StdEncoding.DecodeString(strings.TrimSpace(*pub)); err2 == nil {
				pubB = p
			} else {
				die("verify:", err)
			}
		}
		if len(pubB) == ed25519.PrivateKeySize {
			pubB = pubB[ed25519.PrivateKeySize-ed25519.PublicKeySize:]
		}
		raw, err := os.ReadFile(*in)
		if err != nil {
			die("verify:", err)
		}
		sigB, err := os.ReadFile(*sig)
		if err != nil {
			die("verify:", err)
		}
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigB)))
		if err != nil {
			die("verify:", err)
		}
		if !ed25519.Verify(ed25519.PublicKey(pubB), raw, decoded) {
			die("verify: INVALID signature", nil)
		}
		fmt.Println("signature OK:", *in)
	default:
		usage()
	}
}

func readPriv(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		// create on the fly for development releases
		priv, pub, err := licenses.LoadOrCreateKey(path)
		if err != nil {
			return nil, err
		}
		_ = os.WriteFile(path+".pub", pub, 0o644)
		return priv, nil
	}
	if len(raw) == ed25519.PrivateKeySize {
		return ed25519.PrivateKey(raw), nil
	}
	return nil, fmt.Errorf("%s is not an ed25519 private key", filepath.Base(path))
}

func usage() {
	fmt.Println("uso: selloctl <keys|sign|verify>")
	os.Exit(2)
}

func die(msg string, err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, msg, err)
	} else {
		fmt.Fprintln(os.Stderr, msg)
	}
	os.Exit(1)
}
