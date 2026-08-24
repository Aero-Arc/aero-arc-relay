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

The relay serves the external Agent gateway and Relay control API on the same
gRPC listener. Mutating `SetOperationContext` and `ClearOperationContext` calls
remain disabled unless `control_auth` enables client-certificate verification
and explicitly allow-lists the trusted API workload identity. Agents do not need
client certificates; callers without a verified certificate are rejected from
mutating methods before request validation.

1. Apply default-deny ingress and allow only Agent networks and the trusted Aero
   Arc API workload to reach the listener.
2. Mount the control client CA and set `control_auth.client_ca_file`.
3. Allow only the API certificate's CN, DNS SAN, or URI SAN through
   `control_auth.allowed_identities`.
4. Keep operator/aircraft/Agent authorization in the API before it forwards a
   command, and rotate workload certificates through the cluster issuer.

Do not expose RelayControl mutation access through an external Gateway.
