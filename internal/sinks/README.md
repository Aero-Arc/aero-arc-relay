# Aero Arc Relay - Data Sinks

This directory contains the data sink implementations for the Aero Arc Relay system. Data sinks are responsible for persisting, streaming, and analyzing telemetry data received from MAVLink-enabled devices.

## Overview

The Aero Arc Relay uses a **sink-based architecture** where telemetry data flows from MAVLink sources through the relay to multiple configured data sinks. Each sink is designed for specific use cases and data patterns, allowing users to choose the right storage and analytics solutions for their needs.

## Architecture

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   MAVLink       │    │   Aero Arc      │    │   Data Sinks    │
│   Sources       │───▶│   Relay         │───▶│   (This Dir)    │
│   (Drones, etc) │    │   (Processing)  │    │                 │
└─────────────────┘    └─────────────────┘    └─────────────────┘
```

### Data Flow

1. **MAVLink Messages** are received from drones, ground stations, and edge agents
2. **Telemetry Parser** converts MAVLink messages to standardized `TelemetryMessage` objects
3. **Sink Router** distributes messages to all configured sinks
4. **Data Sinks** persist, stream, or analyze the telemetry data

## Sink Interface

All sinks implement the `Sink` interface:

```go
type Sink interface {
    WriteMessage(msg telemetry.TelemetryMessage) error
    Close(ctx context.Context) error
}
```

## Available Sinks

### 🌐 Cloud Storage Sinks

#### **AWS S3 Sink** (`s3.go`)
- **Purpose**: Long-term archival and backup storage
- **Use Cases**: Data lakes, compliance storage, disaster recovery
- **Features**: Hierarchical organization, metadata attachment, cost-effective storage
- **Best For**: Large-scale data archival, regulatory compliance

#### **Google Cloud Storage Sink** (`gcs.go`)
- **Purpose**: Google Cloud object storage
- **Use Cases**: GCP ecosystem integration, cross-cloud backup
- **Features**: Service account authentication, metadata, organized structure
- **Best For**: GCP-native applications, multi-cloud strategies

### 📊 Analytics & Data Warehousing Sinks

#### **Google BigQuery Sink** (`bigquery.go`)
- **Purpose**: Serverless data warehouse for analytics
- **Use Cases**: Business intelligence, dashboards, SQL analytics
- **Features**: Streaming inserts, batching, rich schema, SQL querying
- **Best For**: Real-time analytics, business intelligence, data science

#### **AWS Timestream Sink** (`timestream.go`)
- **Purpose**: Time-series database optimized for IoT data
- **Use Cases**: Fleet monitoring, IoT analytics, time-series dashboards
- **Features**: Time-series optimization, dimensions + measurements, SQL querying
- **Best For**: IoT analytics, fleet management, time-series analysis

### 📈 Time-Series & Monitoring Sinks

#### **InfluxDB Sink** (`influxdb.go`)
- **Purpose**: Time-series database for high-frequency data
- **Use Cases**: Real-time monitoring, performance analytics, alerting
- **Features**: High-performance writes, time-series optimization, flexible schema
- **Best For**: Real-time monitoring, performance analysis, high-frequency data

#### **Prometheus Sink** (`prometheus.go`)
- **Purpose**: Metrics collection and monitoring
- **Use Cases**: System monitoring, alerting, dashboards
- **Features**: Label-based metrics, high-performance collection, alerting integration
- **Best For**: System monitoring, alerting, operational dashboards

### 🔍 Search & Log Analytics Sinks

#### **Elasticsearch Sink** (`elasticsearch.go`)
- **Purpose**: Search and log analytics platform
- **Use Cases**: Log analysis, search, debugging, security analysis
- **Features**: Full-text search, structured logging, real-time indexing
- **Best For**: Log analysis, debugging, security monitoring, search

### 🚀 Streaming Sinks

#### **Apache Kafka Sink** (`kafka.go`)
- **Purpose**: Real-time streaming platform
- **Use Cases**: Real-time processing, microservices integration, event streaming
- **Features**: High-throughput messaging, topic-based routing, durability
- **Best For**: Real-time processing, microservices, event-driven architectures

### 💾 Local Storage Sinks

#### **File Sink** (`file.go`)
- **Purpose**: Local file-based storage with rotation
- **Use Cases**: Local logging, debugging, offline analysis
- **Features**: Multiple formats (JSON, CSV, Binary), rotation, compression
- **Best For**: Development, debugging, offline analysis, edge deployments

## Sink Selection Guide

### **For Real-Time Monitoring**
- **Prometheus** - Metrics collection and alerting
- **InfluxDB** - High-frequency time-series data
- **Kafka** - Real-time streaming to other systems

### **For Analytics & Business Intelligence**
- **BigQuery** - SQL analytics and business intelligence
- **Timestream** - Time-series analytics and fleet monitoring
- **Elasticsearch** - Log analysis and search

### **For Long-Term Storage**
- **S3** - Cost-effective cloud storage
- **GCS** - Google Cloud storage
- **File** - Local storage with rotation

### **For Search & Debugging**
- **Elasticsearch** - Full-text search and log analysis
- **File** - Local debugging and analysis

## Configuration

Each sink is configured in the main `config.yaml` file:

```yaml
sinks:
  # Cloud Storage
  s3:
    bucket: "your-telemetry-bucket"
    region: "us-west-2"
    access_key: "${AWS_ACCESS_KEY_ID}"
    secret_key: "${AWS_SECRET_ACCESS_KEY}"
    prefix: "telemetry"
  
  gcs:
    bucket: "your-gcs-telemetry-bucket"
    project_id: "your-gcp-project"
    credentials: "/path/to/service-account.json"
    prefix: "telemetry"
  
  # Analytics
  bigquery:
    project_id: "your-gcp-project"
    dataset: "telemetry"
    table: "mavlink_messages"
    credentials: "/path/to/service-account.json"
    batch_size: 1000
    flush_interval: "30s"
  
  timestream:
    database: "telemetry"
    table: "mavlink_messages"
    region: "us-west-2"
    access_key: "${AWS_ACCESS_KEY_ID}"
    secret_key: "${AWS_SECRET_ACCESS_KEY}"
    batch_size: 100
    flush_interval: "30s"
  
  # Time-Series & Monitoring
  influxdb:
    url: "http://localhost:8086"
    database: "telemetry"
    username: "admin"
    password: "password"
    batch_size: 1000
    flush_interval: "30s"
  
  prometheus:
    url: "http://localhost:9090"
    job: "aero-arc-relay"
    instance: "drone-fleet"
    batch_size: 1000
    flush_interval: "30s"
  
  # Search & Logs
  elasticsearch:
    urls: ["http://localhost:9200"]
    index: "mavlink-telemetry"
    username: "elastic"
    password: "password"
    batch_size: 1000
    flush_interval: "30s"
  
  # Streaming
  kafka:
    brokers: ["localhost:9092"]
    topic: "telemetry-data"
  
  # Local Storage
  file:
    path: "/var/log/telemetry"
    format: "json"
    rotation: "daily"
```

## Performance Characteristics

### **High-Throughput Sinks**
- **Kafka**: 100K+ messages/second
- **InfluxDB**: 50K+ points/second
- **File**: 10K+ messages/second

### **Analytics Sinks**
- **BigQuery**: 1K+ rows/second (streaming)
- **Timestream**: 1K+ records/second
- **Elasticsearch**: 5K+ documents/second

### **Monitoring Sinks**
- **Prometheus**: 10K+ samples/second
- **S3/GCS**: 1K+ objects/second

## Batching & Flushing

Most sinks implement **batching and background flushing** for optimal performance:

- **Batch Size**: Configurable number of messages to batch before writing
- **Flush Interval**: Time-based flushing (e.g., every 30 seconds)
- **Background Processing**: Non-blocking writes to prevent backpressure

## Error Handling

All sinks implement robust error handling:

- **Retry Logic**: Automatic retry on transient failures
- **Circuit Breakers**: Prevent cascading failures
- **Graceful Degradation**: Continue processing other sinks if one fails
- **Error Logging**: Comprehensive error reporting and monitoring

## Testing

Each sink includes comprehensive tests:

- **Unit Tests**: Individual sink functionality
- **Integration Tests**: End-to-end data flow
- **Performance Tests**: Throughput and latency validation
- **Error Tests**: Failure scenario handling

## Adding New Sinks

To add a new sink:

1. **Implement the `Sink` interface**:
   ```go
   type MySink struct {
       // sink-specific fields
   }
   
   func (s *MySink) WriteMessage(msg telemetry.TelemetryMessage) error {
       // implementation
   }
   
   func (s *MySink) Close(ctx context.Context) error {
       // cleanup
   }
   ```

2. **Add configuration structure** in `internal/config/config.go`
3. **Add sink initialization** in `internal/relay/relay.go`
4. **Create tests** in `*_test.go`
5. **Update documentation** and examples

## Best Practices

### **Sink Selection**
- **Use multiple sinks** for different use cases
- **Choose sinks based on data patterns** (time-series vs. analytics vs. storage)
- **Consider cost and performance** requirements

### **Configuration**
- **Use environment variables** for sensitive data
- **Set appropriate batch sizes** for your throughput needs
- **Configure flush intervals** based on latency requirements

### **Monitoring**
- **Monitor sink health** and performance
- **Set up alerts** for sink failures
- **Track throughput** and error rates

## Troubleshooting

### **Common Issues**
- **Connection failures**: Check network connectivity and credentials
- **Performance issues**: Adjust batch sizes and flush intervals
- **Memory usage**: Monitor buffer sizes and cleanup

### **Debugging**
- **Enable debug logging** for detailed sink operations
- **Use file sink** for local debugging
- **Monitor sink metrics** for performance analysis

---

For more information, see the main [README.md](../../README.md) and [Configuration Guide](../../configs/config.yaml.example).
