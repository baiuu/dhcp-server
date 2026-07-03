package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dhcp-server/dhcp-server/internal/config"
	"github.com/dhcp-server/dhcp-server/internal/models"
	"github.com/dhcp-server/dhcp-server/internal/store"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const (
	ContextUserKey = "auth_user"
)

type Service struct {
	cfg        *config.Config
	store      *store.Store
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
}

func NewService(cfg *config.Config, store *store.Store) (*Service, error) {
	if err := ensureRSAKeys(cfg.Auth.RSAPrivateKeyFile, cfg.Auth.RSAPublicKeyFile); err != nil {
		return nil, fmt.Errorf("ensure rsa keys: %w", err)
	}
	privateKey, err := loadRSAPrivateKey(cfg.Auth.RSAPrivateKeyFile)
	if err != nil {
		return nil, fmt.Errorf("load rsa private key: %w", err)
	}
	publicKey, err := loadRSAPublicKey(cfg.Auth.RSAPublicKeyFile)
	if err != nil {
		return nil, fmt.Errorf("load rsa public key: %w", err)
	}
	return &Service{
		cfg:        cfg,
		store:      store,
		privateKey: privateKey,
		publicKey:  publicKey,
	}, nil
}

// ensureRSAKeys checks whether the RSA key pair exists. If either file is
// missing, it generates a new 2048-bit key pair and writes both PEM files.
func ensureRSAKeys(privatePath, publicPath string) error {
	_, privErr := os.Stat(privatePath)
	_, pubErr := os.Stat(publicPath)
	if privErr == nil && pubErr == nil {
		return nil
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("generate rsa key: %w", err)
	}

	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	pubBytes, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return fmt.Errorf("marshal public key: %w", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubBytes,
	})

	if err := os.MkdirAll(filepath.Dir(privatePath), 0755); err != nil {
		return fmt.Errorf("create private key dir: %w", err)
	}
	if err := os.WriteFile(privatePath, privPEM, 0600); err != nil {
		return fmt.Errorf("write private key: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(publicPath), 0755); err != nil {
		return fmt.Errorf("create public key dir: %w", err)
	}
	if err := os.WriteFile(publicPath, pubPEM, 0644); err != nil {
		return fmt.Errorf("write public key: %w", err)
	}

	return nil
}

func loadRSAPrivateKey(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("invalid private key pem")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err == nil {
		return key, nil
	}
	key2, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rsaKey, ok := key2.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not rsa")
	}
	return rsaKey, nil
}

func loadRSAPublicKey(path string) (*rsa.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("invalid public key pem")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("public key is not rsa")
	}
	return rsaPub, nil
}

func (s *Service) InitializeDefaultAdmin(ctx context.Context) error {
	if s.cfg.Auth.DefaultAdminUsername == "" || s.cfg.Auth.DefaultAdminPassword == "" {
		return nil
	}
	count, err := s.store.CountUsers(ctx)
	if err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if count > 0 {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(s.cfg.Auth.DefaultAdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	user := &models.User{
		ID:           uuid.New().String(),
		Username:     s.cfg.Auth.DefaultAdminUsername,
		PasswordHash: string(hash),
		Role:         "admin",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	return s.store.CreateUser(ctx, user)
}

func (s *Service) Authenticate(ctx context.Context, username, password string) (string, string, error) {
	user, err := s.store.GetUserByUsername(ctx, username)
	if err != nil {
		return "", "", errors.New("用户名或密码错误")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return "", "", errors.New("用户名或密码错误")
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub":  user.ID,
		"usr":  user.Username,
		"role": user.Role,
		"exp":  time.Now().UTC().Add(s.cfg.Auth.TokenTTL).Unix(),
		"iat":  time.Now().UTC().Unix(),
		"jti":  uuid.New().String(),
	})
	tokenStr, err := token.SignedString(s.privateKey)
	if err != nil {
		return "", "", err
	}
	return tokenStr, user.Role, nil
}

func (s *Service) ValidateToken(tokenString string) (*models.User, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.publicKey, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token claims")
	}
	return &models.User{
		ID:       getString(claims, "sub"),
		Username: getString(claims, "usr"),
		Role:     getString(claims, "role"),
	}, nil
}

func getString(claims jwt.MapClaims, key string) string {
	v, ok := claims[key].(string)
	if !ok {
		return ""
	}
	return v
}

func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (s *Service) VerifyPassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}
