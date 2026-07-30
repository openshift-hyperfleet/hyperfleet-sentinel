package queue

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type QueueMessage struct {
	ID              string    `gorm:"primaryKey;size:255"`
	ResourceID      string    `gorm:"size:255;not null"`
	Kind            string    `gorm:"size:100;not null"`
	Href            string    `gorm:"size:500"`
	Generation      int32     `gorm:"not null"`
	OwnerReferences string    `gorm:"type:jsonb"`
	EventType       string    `gorm:"size:255;not null"`
	CreatedAt       time.Time `gorm:"not null;default:now()"`
}

func (QueueMessage) TableName() string {
	return "reconciliation_queue"
}

type Publisher struct {
	db *gorm.DB
}

func NewPublisher(db *gorm.DB) *Publisher {
	return &Publisher{db: db}
}

func (p *Publisher) Publish(ctx context.Context, msg *QueueMessage) error {
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("failed to generate message ID: %w", err)
	}
	msg.ID = id.String()
	msg.CreatedAt = time.Now().UTC()

	if err := p.db.WithContext(ctx).Create(msg).Error; err != nil {
		return fmt.Errorf("failed to insert queue message: %w", err)
	}
	return nil
}

func (p *Publisher) Health(ctx context.Context) error {
	sqlDB, err := p.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(&QueueMessage{})
}
