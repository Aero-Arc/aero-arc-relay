### Kubernetes Example

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: aero-arc-relay
spec:
  replicas: 1
  template:
    spec:
      containers:
      - name: relay
        image: aeroarc/relay:latest
        ports:
        - containerPort: 14550
          protocol: UDP
        - containerPort: 2112
          protocol: TCP
        env:
        - name: AWS_ACCESS_KEY_ID
          valueFrom:
            secretKeyRef:
              name: aws-credentials
              key: access-key-id
        - name: AWS_SECRET_ACCESS_KEY
          valueFrom:
            secretKeyRef:
              name: aws-credentials
              key: secret-access-key
        livenessProbe:
          httpGet:
            path: /healthz
            port: 2112
          initialDelaySeconds: 10
        readinessProbe:
          httpGet:
            path: /readyz
            port: 2112
          initialDelaySeconds: 5
```