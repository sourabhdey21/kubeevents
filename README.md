# KubeEvents

Watch Kubernetes cluster events (pod create/delete, deployment changes, failures, scheduling issues, and more) and send live notifications to a **Telegram channel**.

Designed for simple cluster install via **Helm** or plain manifests.

## What you get

- Watches the Kubernetes **Events** API cluster-wide (or one namespace)
- Forwards create / update / delete related activity as Telegram messages
- Filters noisy Normal events (image pulls, scheduled, etc.) by default
- Deduplication + rate limiting so busy clusters do not flood Telegram
- Health probes, least-privilege RBAC, distroless image

Example Telegram message:

```
⚠️ Failed — Warning
🏷 Cluster: prod-cluster
📦 Pod/nginx-7d8f9c
🗂 Namespace: default
🔧 Source: kubelet
🕒 2026-08-01T20:15:00Z

Error: ImagePullBackOff
```

---

## Web UI

KubeEvents ships with a live web console. After install:

```bash
kubectl -n kubeevents port-forward svc/kubeevents 8080:80
```

Open [http://127.0.0.1:8080](http://127.0.0.1:8080) to browse events with filters, search, and live updates (SSE).

Optional Ingress:

```bash
helm upgrade --install kubeevents ./deploy/helm/kubeevents \
  -n kubeevents --create-namespace \
  --set telegram.botToken="$TELEGRAM_BOT_TOKEN" \
  --set telegram.chatId="$TELEGRAM_CHAT_ID" \
  --set ingress.enabled=true \
  --set ingress.hosts[0].host=kubeevents.example.com
```

---

## 1. Create a Telegram bot & channel

1. Open Telegram and chat with [@BotFather](https://t.me/BotFather)
2. Send `/newbot` and follow the prompts → copy the **bot token**
3. Create a channel (or group) for alerts
4. Add the bot as an **administrator** of that channel
5. Get the **chat ID**:
   - Forward any channel message to [@userinfobot](https://t.me/userinfobot), **or**
   - Post something in the channel, then open:
     `https://api.telegram.org/bot<TOKEN>/getUpdates`
   - Channel IDs look like `-100xxxxxxxxxx`

Keep both values ready:

| Variable | Example |
|---|---|
| `TELEGRAM_BOT_TOKEN` | `7123456789:AAH...` |
| `TELEGRAM_CHAT_ID` | `-1001234567890` |

---

## 2. Build the image

```bash
# From this repository root
docker build -t sourabhdey21700/kubeevents:0.2.0 .
docker tag sourabhdey21700/kubeevents:0.2.0 sourabhdey21700/kubeevents:latest
```

If your cluster cannot pull from your laptop, load the image into the cluster (kind / minikube / k3s examples):

```bash
# kind
kind load docker-image kubeevents:0.1.0

# minikube
minikube image load kubeevents:0.1.0

# k3s (save/import)
docker save kubeevents:0.1.0 | sudo k3s ctr images import -
```

---

## 3. Install (recommended: Helm)

```bash
helm upgrade --install kubeevents ./deploy/helm/kubeevents \
  --namespace kubeevents --create-namespace \
  --set telegram.botToken="$TELEGRAM_BOT_TOKEN" \
  --set telegram.chatId="$TELEGRAM_CHAT_ID" \
  --set clusterName="my-cluster" \
  --set image.repository=sourabhdey21700/kubeevents \
  --set image.tag=0.2.0 \
  --set image.pullPolicy=Always
```

Or use a values file (see `deploy/helm/kubeevents/values-example.yaml`):

```bash
helm upgrade --install kubeevents ./deploy/helm/kubeevents \
  -n kubeevents --create-namespace \
  -f my-values.yaml
```

### Useful Helm options

| Setting | Default | Meaning |
|---|---|---|
| `watch.minSeverity` | `all` | Use `warning` to only get Warning events |
| `watch.namespace` | `""` | Set e.g. `production` to watch one namespace |
| `watch.resourceKinds` | `""` | e.g. `Pod,Deployment,Service,Node` |
| `watch.skipNoisyNormals` | `true` | Drop Pulling/Scheduled/etc. |
| `watch.maxMessagesPerMinute` | `30` | Telegram rate limit |

---

## 4. Install (plain manifests)

```bash
# 1) Edit credentials
sed -i 's|REPLACE_WITH_BOT_TOKEN|'"$TELEGRAM_BOT_TOKEN"'|' deploy/kubernetes/secret.yaml
sed -i 's|REPLACE_WITH_CHAT_ID|'"$TELEGRAM_CHAT_ID"'|' deploy/kubernetes/secret.yaml

# 2) Optionally edit ConfigMap (cluster name, filters)
#    deploy/kubernetes/configmap.yaml

# 3) Point Deployment at your image if needed
#    deploy/kubernetes/deployment.yaml

# 4) Apply
kubectl apply -f deploy/kubernetes/
```

---

## 5. Verify

```bash
kubectl get pods -n kubeevents
kubectl logs -n kubeevents -l app.kubernetes.io/name=kubeevents -f
```

You should get a **startup message** in Telegram.

Generate a test event:

```bash
kubectl run nginx-test --image=nginx -n default
kubectl delete pod nginx-test -n default
```

---

## Configuration reference (env vars)

| Env var | Default | Description |
|---|---|---|
| `TELEGRAM_BOT_TOKEN` | *(required)* | Bot token from BotFather |
| `TELEGRAM_CHAT_ID` | *(required)* | Channel / group chat ID |
| `CLUSTER_NAME` | `kubernetes` | Label in messages |
| `WATCH_NAMESPACE` | *(all)* | Restrict to one namespace |
| `MIN_SEVERITY` | `all` | `all` or `warning` |
| `EVENT_TYPES` | *(both)* | `Normal`, `Warning` |
| `RESOURCE_KINDS` | *(all)* | Comma-separated kinds |
| `SKIP_NOISY_NORMALS` | `true` | Filter routine Normal events |
| `DEDUP_WINDOW` | `2m` | Suppress identical repeats |
| `MAX_MESSAGES_PER_MINUTE` | `30` | Rate limit |
| `SEND_STARTUP_MESSAGE` | `true` | Ping Telegram on boot |
| `HEALTH_ADDR` | `:8080` | Probe listen address |

---

## Uninstall

```bash
# Helm
helm uninstall kubeevents -n kubeevents
kubectl delete ns kubeevents

# Manifests
kubectl delete -f deploy/kubernetes/
```

---

## Project layout

```
cmd/kubeevents/          # entrypoint
internal/config/         # env configuration
internal/filter/         # severity / kind / dedup filters
internal/notifier/       # Telegram client
internal/watcher/        # Kubernetes Events informer
deploy/helm/kubeevents/  # Helm chart
deploy/kubernetes/       # plain YAML manifests
Dockerfile
```

## License

MIT
