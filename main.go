package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type Product struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	Name      string    `json:"name"`
	Price     float64   `json:"price"`
	CreatedAt time.Time `json:"created_at"`
}

func initDB() *gorm.DB {
	dsn := "host=localhost user=postgres password=postgres dbname=test port=5432 sslmode=disable"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		panic("failed to connect database")
	}

	db.AutoMigrate(&Product{})
	return db
}

func main() {
	db := initDB()
	r := gin.Default()

	r.GET("/products/offset", offsetPagination(db))
	r.GET("/products/cursor", cursorPagination(db))
	r.GET("/products/seek", seekPagination(db))
	r.GET("/products/token", tokenPagination(db))

	r.Run(":8080")
}

// 1. Phương pháp Offset-based Pagination
func offsetPagination(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var products []Product
		page := 1
		if p := c.Query("page"); p != "" {
			fmt.Sscanf(p, "%d", &page)
		}
		pageSize := 10
		if ps := c.Query("page_size"); ps != "" {
			fmt.Sscanf(ps, "%d", &pageSize)
		}

		// Tính offset
		offset := (page - 1) * pageSize

		// Đếm tổng số bản ghi
		var total int64
		db.Model(&Product{}).Count(&total)

		// Lấy dữ liệu với phân trang
		result := db.Offset(offset).Limit(pageSize).Order("id").Find(&products)
		if result.Error != nil {
			c.JSON(500, gin.H{"error": result.Error.Error()})
			return
		}

		// Trả về kết quả và thông tin phân trang
		c.JSON(200, gin.H{
			"data":       products,
			"total":      total,
			"page":       page,
			"page_size":  pageSize,
			"total_page": (total + int64(pageSize) - 1) / int64(pageSize),
		})
	}
}

// 2. Phương pháp Cursor-based Pagination
func cursorPagination(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var products []Product
		limit := 10
		if l := c.Query("limit"); l != "" {
			fmt.Sscanf(l, "%d", &limit)
		}

		// Lấy cursor từ query param
		cursor := c.Query("cursor")
		var lastID uint

		if cursor != "" {
			fmt.Sscanf(cursor, "%d", &lastID)
		}

		// Query với cursor
		query := db.Model(&Product{})
		if lastID > 0 {
			query = query.Where("id > ?", lastID)
		}

		// Lấy dữ liệu
		result := query.Order("id").Limit(limit + 1).Find(&products)
		if result.Error != nil {
			c.JSON(500, gin.H{"error": result.Error.Error()})
			return
		}

		// Xác định cursor tiếp theo
		var nextCursor string
		hasMore := false

		if len(products) > limit {
			hasMore = true
			nextCursor = fmt.Sprintf("%d", products[limit-1].ID)
			products = products[:limit]
		}

		c.JSON(200, gin.H{
			"data":        products,
			"next_cursor": nextCursor,
			"has_more":    hasMore,
		})
	}
}

// 3. Phương pháp Seek-based Pagination
func seekPagination(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var products []Product
		limit := 10
		if l := c.Query("limit"); l != "" {
			fmt.Sscanf(l, "%d", &limit)
		}

		// Lấy các tham số phân trang
		lastID := c.Query("last_id")
		lastCreatedAt := c.Query("last_created_at")

		// Query với điều kiện seek
		query := db.Model(&Product{})
		if lastID != "" && lastCreatedAt != "" {
			var lastIDUint uint
			fmt.Sscanf(lastID, "%d", &lastIDUint)

			parsedTime, err := time.Parse(time.RFC3339, lastCreatedAt)
			if err != nil {
				c.JSON(400, gin.H{"error": "Invalid datetime format"})
				return
			}

			// Sử dụng created_at và id để seek
			query = query.Where(
				"(created_at, id) > (?, ?)",
				parsedTime,
				lastIDUint,
			)
		}

		// Lấy dữ liệu
		result := query.Order("created_at").Order("id").Limit(limit + 1).Find(&products)
		if result.Error != nil {
			c.JSON(500, gin.H{"error": result.Error.Error()})
			return
		}

		// Xác định tham số tiếp theo
		var nextLastID string
		var nextLastCreatedAt string
		hasMore := false

		if len(products) > limit {
			hasMore = true
			nextLastID = fmt.Sprintf("%d", products[limit-1].ID)
			nextLastCreatedAt = products[limit-1].CreatedAt.Format(time.RFC3339)
			products = products[:limit]
		}

		c.JSON(200, gin.H{
			"data":                 products,
			"next_last_id":         nextLastID,
			"next_last_created_at": nextLastCreatedAt,
			"has_more":             hasMore,
		})
	}
}

// 4. Phương pháp Token-based Pagination
type PaginationToken struct {
	LastID        uint      `json:"last_id"`
	LastCreatedAt time.Time `json:"last_created_at"`
	Page          int       `json:"page"`
}

func tokenPagination(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var products []Product
		limit := 10
		if l := c.Query("limit"); l != "" {
			fmt.Sscanf(l, "%d", &limit)
		}

		// Parse token từ query param
		tokenStr := c.Query("token")
		var token PaginationToken

		if tokenStr != "" {
			// Decode token
			tokenBytes, err := base64.StdEncoding.DecodeString(tokenStr)
			if err != nil {
				c.JSON(400, gin.H{"error": "Invalid token"})
				return
			}

			err = json.Unmarshal(tokenBytes, &token)
			if err != nil {
				c.JSON(400, gin.H{"error": "Invalid token format"})
				return
			}
		}

		// Query với token
		query := db.Model(&Product{})
		if token.LastID > 0 {
			query = query.Where(
				"(created_at, id) > (?, ?)",
				token.LastCreatedAt,
				token.LastID,
			)
		}

		// Lấy dữ liệu
		result := query.Order("created_at").Order("id").Limit(limit + 1).Find(&products)
		if result.Error != nil {
			c.JSON(500, gin.H{"error": result.Error.Error()})
			return
		}

		// Tạo token tiếp theo
		var nextToken string
		hasMore := false

		if len(products) > limit {
			hasMore = true
			newToken := PaginationToken{
				LastID:        products[limit-1].ID,
				LastCreatedAt: products[limit-1].CreatedAt,
				Page:          token.Page + 1,
			}

			// Encode token
			tokenBytes, _ := json.Marshal(newToken)
			nextToken = base64.StdEncoding.EncodeToString(tokenBytes)

			products = products[:limit]
		}

		c.JSON(200, gin.H{
			"data":       products,
			"next_token": nextToken,
			"has_more":   hasMore,
			"page":       token.Page + 1,
		})
	}
}
