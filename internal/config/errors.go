package config

import "fmt"

var (
	ErrNoEndpoints             = fmt.Errorf("no MAVLink endpoints configured")
	ErrInvalidProtocol         = fmt.Errorf("invalid MAVLink endpoint protocol")
	ErrInvalidMode             = fmt.Errorf("invalid MAVLink endpoint mode")
	ErrInvalidName             = fmt.Errorf("invalid MAVLink endpoint name")
	ErrInvalidBaudRate         = fmt.Errorf("invalid MAVLink endpoint baud rate")
	ErrInvalidPort             = fmt.Errorf("invalid MAVLink endpoint port")
	ErrInvalidAddress          = fmt.Errorf("invalid MAVLink endpoint address")
	ErrInvalidCredentials      = fmt.Errorf("invalid MAVLink endpoint credentials")
	ErrInvalidPrefix           = fmt.Errorf("invalid MAVLink endpoint prefix")
	ErrInvalidFlushInterval    = fmt.Errorf("invalid MAVLink endpoint flush interval")
	ErrInvalidQueueSize        = fmt.Errorf("invalid MAVLink endpoint queue size")
	Err1To1ModeRequiresDroneID = fmt.Errorf("1:1 mode requires DroneID to be configured")
	ErrFailedToParseConfigFile = fmt.Errorf("failed to parse config file")
	ErrFailedToReadConfigFile  = fmt.Errorf("failed to read config file")
	ErrNoValidEndpoints        = fmt.Errorf("no valid MAVLink endpoints configured")
	ErrInvalidDialect          = fmt.Errorf("invalid MAVLink dialect")
	ErrDroneIDRequired         = fmt.Errorf("drone ID is required for 1:1 mode")
	ErrMultiModeNotSupported   = fmt.Errorf("multi mode is not supported until agent is implemented")
)
