package verifyimage

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/sigstore/cosign/v2/pkg/cosign"
	"github.com/sigstore/cosign/v2/pkg/signature"
)

// defaultCosignPublicKeyPEM is a built-in fallback public key.
//
// Security note:
//   - Hardcoding a *public* key is acceptable for demos and tightly controlled environments.
//   - In production, prefer injecting the key via Kubernetes Secret -> env (COSIGN_PUB_KEY_PEM),
//     so you can rotate the key without rebuilding the controller image.
const defaultCosignPublicKeyPEM = ` 
-----BEGIN PUBLIC KEY-----
 Không khuyến nghị hardcode khóa công khai trong mã nguồn. Vui lòng sử dụng biến môi trường hoặc bí mật Kubernetes để quản lý khóa công khai một cách an toàn.
-----END PUBLIC KEY-----

`

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func buildCosignCheckOpts(ctx context.Context) (*cosign.CheckOpts, func(), error) {
	// Preferred path: load the PEM content from an env var injected by Kubernetes.
	// In this repo, the controller Deployment maps:
	//   env: COSIGN_PUB_KEY_PEM <- secretKeyRef(name=cosign-pub-key, key=cosign.pub)
	pem := strings.TrimSpace(os.Getenv("COSIGN_PUB_KEY_PEM"))
	if pem == "" {
		// Fallback: use the built-in default key.
		pem = strings.TrimSpace(defaultCosignPublicKeyPEM)
	}
	if pem != "" {
		f, err := os.CreateTemp("", "cosign-pub-*.pem")
		if err != nil {
			return nil, nil, fmt.Errorf("create temp file for cosign public key: %w", err)
		}
		cleanup := func() {
			_ = f.Close()
			_ = os.Remove(f.Name())
		}
		if _, err := io.WriteString(f, pem+"\n"); err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("write cosign public key PEM to temp file: %w", err)
		}
		if err := f.Close(); err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("close cosign public key temp file: %w", err)
		}

		verifier, err := signature.LoadPublicKey(ctx, f.Name())
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("load cosign public key from COSIGN_PUB_KEY_PEM: %w", err)
		}
		return &cosign.CheckOpts{SigVerifier: verifier}, cleanup, nil
	}

	// Backward-compatible fallback for local dev tooling.
	// If you want to forbid filesystem keys entirely, remove this block.
	pubKeyPath := getenv("COSIGN_PUB_KEY", "./cosign.pub")
	verifier, err := signature.LoadPublicKey(ctx, pubKeyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("cosign public key not found in env COSIGN_PUB_KEY_PEM and failed to load from path %q: %w", pubKeyPath, err)
	}
	return &cosign.CheckOpts{SigVerifier: verifier}, func() {}, nil
}

func VerifyImageSignature(image string) error {

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	img := strings.TrimSpace(image)
	if img == "" {
		return fmt.Errorf("empty image")
	}

	if strings.HasSuffix(img, ".sig") && strings.Contains(img, ":sha256-") {
		return fmt.Errorf("image looks like a cosign signature artifact tag (ends with .sig); verify the real image tag/digest instead, e.g. repo:tag or repo@sha256:...")
	}
	ref, err := name.ParseReference(image)
	if err != nil {
		return err
	}
	co, cleanup, err := buildCosignCheckOpts(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	if rekorPubs, e := cosign.GetRekorPubs(ctx); e == nil {
		co.RekorPubKeys = rekorPubs
	} else {
		if getenv("COSIGN_IGNORE_TLOG", "false") == "true" {
			co.IgnoreTlog = true
			log.Printf("warning: cannot load Rekor public keys (%v); COSIGN_IGNORE_TLOG=true so skipping tlog verification", e)
		} else {
			return fmt.Errorf("cannot load Rekor public keys (needed to verify bundle): %w (set COSIGN_IGNORE_TLOG=true to skip tlog verification)", e)
		}
	}

	_, _, err = cosign.VerifyImageSignatures(ctx, ref, co)
	if err != nil {
		return fmt.Errorf("verify failed for %q: %w", img, err)
	}
	return nil

}

// func main() {
// 	image := "shieldxbot/backend_example:v1.0.0"
// 	err := VerifyImageSignature(image)
// 	if err != nil {
// 		log.Fatalf("Image signature verification failed: %v", err)
// 	} else {
// 		log.Printf("Image signature verification succeeded for %s", image)
// 	}
// }
