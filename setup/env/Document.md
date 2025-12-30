# Quy trình cấu hình và kiểm tra biến môi trường

## 1. Thiết lập biến môi trường trong  shieldx-platform/config/manager/manager.yaml



``` bash 

 containers:
        - command:
            - /manager
          args:
            - --leader-elect
            - --health-probe-bind-address=:8081
          image: controller:latest
          name: manager
          env:
            - name: TELEGRAM_BOT_TOKEN
              valueFrom:
                secretKeyRef:
                  name: telegram-credentials
                  key: botToken
                  optional: true

            - name: TELEGRAM_CHAT_ID
              valueFrom:
                secretKeyRef:
                  name: telegram-credentials
                  key: chatId
                  optional: true
            - name: COSIGN_PUB_KEY_PEM
              valueFrom:
                secretKeyRef:
                  name: cosign-pub-key
                  key: cosign.pub
                  optional: true
          ports: []

```


## 2. Áp dụng các Secret chứa biến môi trường
``` bash
kubectl apply -f setup/env/telegram-credentials-secret.yaml
kubectl apply -f setup/env/cosign-pub-key-secret.yaml
```



## 3. Kiểm tra biến môi trường đã được tạo trong namespace chưa
``` bash
kubectl get secret telegram-credentials -n shieldx-platform-system -o yaml
kubectl get secret cosign-pub-key -n shieldx-platform-system -o yaml
```


## 4. Kiểm tra biến môi trường đã được mount vào Pod chưa
``` bash
kubectl get pods -n shieldx-platform-system
kubectl describe pod <pod-name> -n shieldx-platform-system
```
