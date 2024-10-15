package main

import (
	"context"
	"log"
	"os"

	"cloud.google.com/go/firestore"
	"github.com/go-playground/validator/v10"
	"github.com/go-resty/resty/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/joho/godotenv"
	"google.golang.org/api/option"
)

type User struct {
	UserID string `json:"userId" validate:"required"`
	Token  string `json:"token" validate:"required"`
}

type NotificationRequest struct {
	Token string            `json:"token" validate:"required"`
	Title string            `json:"title" validate:"required"`
	Body  string            `json:"body" validate:"required"`
	Data  map[string]string `json:"data"`
}

var (
	validate        = validator.New()
	firestoreClient *firestore.Client
)

func main() {
	// Load environment variables
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found")
	}

	// Baca jalur ke file Service Account dari variabel lingkungan
	serviceAccountKeyPath := os.Getenv("SERVICE_ACCOUNT_KEY_PATH")
	if serviceAccountKeyPath == "" {
		log.Fatal("SERVICE_ACCOUNT_KEY_PATH not set")
	}

	// Inisialisasi Firestore dengan Service Account
	ctx := context.Background()
	sa := option.WithCredentialsFile(serviceAccountKeyPath)
	firestoreClient, err = firestore.NewClient(ctx, os.Getenv("FIREBASE_PROJECT_ID"), sa)
	if err != nil {
		log.Fatalf("Failed to create Firestore client: %v", err)
	}
	defer firestoreClient.Close()

	app := fiber.New()

	// Middleware
	app.Use(logger.New())
	app.Use(recover.New())

	// Route untuk menyimpan token FCM ke Firestore.
	app.Post("/send-token", sendTokenHandler)

	// Route untuk mengirim notifikasi ke pengguna.
	app.Post("/send-notification", sendNotificationHandler)

	// Jalankan server
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	log.Fatal(app.Listen(":" + port))
}

// endpoint /send-token
func sendTokenHandler(c *fiber.Ctx) error {
	user := new(User)
	if err := c.BodyParser(user); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Invalid request body",
		})
	}

	// Validasi input
	if err := validate.Struct(user); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Validation failed",
			"error":   err.Error(),
		})
	}

	// Simpan token ke Firestore
	ctx := context.Background()
	_, err := firestoreClient.Collection("users").Doc(user.UserID).Set(ctx, fiber.Map{
		"fcmToken": user.Token,
	}, firestore.MergeAll)
	if err != nil {
		log.Printf("Error saving token to Firestore: %v\n", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Gagal menyimpan token",
			"error":   err.Error(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "Token berhasil disimpan",
	})
}

// endpoint send-notification
func sendNotificationHandler(c *fiber.Ctx) error {
	notif := new(NotificationRequest)
	if err := c.BodyParser(notif); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Invalid request body",
		})
	}

	// Validasi input
	if err := validate.Struct(notif); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Validation failed",
			"error":   err.Error(),
		})
	}

	// Kirim notifikasi melalui FCM
	response, err := sendFCMNotification(notif)
	if err != nil {
		log.Printf("Error sending notification: %v\n", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Gagal mengirim notifikasi",
			"error":   err.Error(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message":  "Notifikasi terkirim",
		"response": response,
	})
}

func sendFCMNotification(notif *NotificationRequest) (map[string]interface{}, error) {
	// Ambil jalur ke file Service Account dari variabel lingkungan
	serviceAccountKeyPath := os.Getenv("SERVICE_ACCOUNT_KEY_PATH")
	if serviceAccountKeyPath == "" {
		return nil, fiber.NewError(fiber.StatusInternalServerError, "SERVICE_ACCOUNT_KEY_PATH not set")
	}

	client := resty.New()

	payload := map[string]interface{}{
		"message": map[string]interface{}{
			"token": notif.Token,
			"notification": map[string]string{
				"title": notif.Title,
				"body":  notif.Body,
			},
			"data": notif.Data,
		},
	}

	var result map[string]interface{}

	resp, err := client.R().
		SetHeader("Content-Type", "application/json").
		SetBody(payload).
		SetResult(&result). // SetResult mengarahkan Resty untuk mem-parsing respons ke dalam `result`
		Post("https://fcm.googleapis.com/v1/projects/" + os.Getenv("FIREBASE_PROJECT_ID") + "/messages:send")

	if err != nil {
		return nil, err
	}

	if resp.IsError() {
		return nil, fiber.NewError(resp.StatusCode(), resp.String())
	}

	// Mengakses hasil yang telah diparse
	return result, nil
}
