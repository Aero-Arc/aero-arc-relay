package telemetry

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"time"
)

// Data represents telemetry data from a drone/vehicle
type Data struct {
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"`
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	Altitude  float64   `json:"altitude"`
	Speed     float64   `json:"speed"`
	Heading   float64   `json:"heading"`
	Battery   float64   `json:"battery,omitempty"`
	Signal    int       `json:"signal_strength,omitempty"`
	Mode      string    `json:"flight_mode,omitempty"`
	Status    string    `json:"status,omitempty"`
}

// New creates a new telemetry data instance
func New(source string) *Data {
	return &Data{
		Timestamp: time.Now(),
		Source:    source,
	}
}

// ToJSON serializes the data to JSON
func (d *Data) ToJSON() ([]byte, error) {
	return json.Marshal(d)
}

// ToBinary serializes the data to binary format
func (d *Data) ToBinary() ([]byte, error) {
	// Simple binary format: timestamp(8) + source_len(4) + source + lat(8) + lon(8) + alt(8) + speed(8) + heading(8)
	sourceBytes := []byte(d.Source)

	data := make([]byte, 8+4+len(sourceBytes)+8+8+8+8+8)
	offset := 0

	// Timestamp (8 bytes)
	binary.BigEndian.PutUint64(data[offset:], uint64(d.Timestamp.Unix()))
	offset += 8

	// Source length (4 bytes)
	binary.BigEndian.PutUint32(data[offset:], uint32(len(sourceBytes)))
	offset += 4

	// Source string
	copy(data[offset:], sourceBytes)
	offset += len(sourceBytes)

	// Latitude (8 bytes)
	binary.BigEndian.PutUint64(data[offset:], uint64(d.Latitude))
	offset += 8

	// Longitude (8 bytes)
	binary.BigEndian.PutUint64(data[offset:], uint64(d.Longitude))
	offset += 8

	// Altitude (8 bytes)
	binary.BigEndian.PutUint64(data[offset:], uint64(d.Altitude))
	offset += 8

	// Speed (8 bytes)
	binary.BigEndian.PutUint64(data[offset:], uint64(d.Speed))
	offset += 8

	// Heading (8 bytes)
	binary.BigEndian.PutUint64(data[offset:], uint64(d.Heading))

	return data, nil
}

// FromBinary deserializes data from binary format
func FromBinary(data []byte) (*Data, error) {
	if len(data) < 8+4+8+8+8+8+8 {
		return nil, fmt.Errorf("insufficient data for binary format")
	}

	offset := 0

	// Timestamp
	timestamp := time.Unix(int64(binary.BigEndian.Uint64(data[offset:])), 0)
	offset += 8

	// Source length
	sourceLen := binary.BigEndian.Uint32(data[offset:])
	offset += 4

	// Source string
	if len(data) < offset+int(sourceLen) {
		return nil, fmt.Errorf("insufficient data for source string")
	}
	source := string(data[offset : offset+int(sourceLen)])
	offset += int(sourceLen)

	// Latitude
	latitude := float64(binary.BigEndian.Uint64(data[offset:]))
	offset += 8

	// Longitude
	longitude := float64(binary.BigEndian.Uint64(data[offset:]))
	offset += 8

	// Altitude
	altitude := float64(binary.BigEndian.Uint64(data[offset:]))
	offset += 8

	// Speed
	speed := float64(binary.BigEndian.Uint64(data[offset:]))
	offset += 8

	// Heading
	heading := float64(binary.BigEndian.Uint64(data[offset:]))

	return &Data{
		Timestamp: timestamp,
		Source:    source,
		Latitude:  latitude,
		Longitude: longitude,
		Altitude:  altitude,
		Speed:     speed,
		Heading:   heading,
	}, nil
}
