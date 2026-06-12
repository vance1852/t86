package auth

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"venue-booking-admin/internal/models"
)

var jwtSecret []byte

// SetSecret 设置 JWT 签名密钥。
func SetSecret(secret string) {
	jwtSecret = []byte(secret)
}

// HashPassword 生成密码哈希。
func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

// VerifyPassword 校验密码。
func VerifyPassword(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// CreateToken 为用户签发 JWT。
func CreateToken(userID uint, username string) (string, error) {
	claims := jwt.MapClaims{
		"sub": userID,
		"usr": username,
		"exp": time.Now().Add(12 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret)
}

// Middleware 鉴权中间件，校验 Bearer Token 并把用户写入上下文。
func Middleware(database *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"detail": "未提供登录凭证"})
			return
		}
		tokenStr := strings.TrimPrefix(header, "Bearer ")
		token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
			return jwtSecret, nil
		})
		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"detail": "登录状态无效或已过期"})
			return
		}
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"detail": "登录状态无效"})
			return
		}
		uid := uint(claims["sub"].(float64))
		var user models.User
		if err := database.First(&user, uid).Error; err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"detail": "用户不存在"})
			return
		}
		c.Set("user", user)
		c.Next()
	}
}
