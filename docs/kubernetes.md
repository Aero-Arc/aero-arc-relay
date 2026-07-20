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

### Control-plane security

Container placement alone is not an authorization boundary. Kubernetes pod
networking commonly permits pod-to-pod traffic unless NetworkPolicy isolates a
workload, and a NetworkPolicy can restrict ports but not individual gRPC methods
on a shared port.

The relay currently serves the external agent gateway and relay control API on
the same gRPC listener. For that reason, the mutating `SetOperationContext` and
`ClearOperationContext` RPCs remain disabled. Before enabling them:

1. Serve the relay control API on a separate internal listener and `ClusterIP`.
2. Apply default-deny ingress and allow only the trusted Aero Arc API workload.
3. Require mutual TLS or equivalent workload authentication on that listener.
4. Authorize the caller for the requested operator, aircraft, and agent before
   forwarding a command.

Do not expose the future control listener through `NodePort`, `LoadBalancer`, or
an external Gateway.
