(ns bifrost.http
  (:require [bifrost.config :as config]
            [bifrost.api :as api]
            [bifrost.routing :as routing]
            [bifrost.ingress :as ingress]
            [bifrost.store :as store]
            [bifrost.wire :as w]
            [promesa.core :as p]))

(defn- read-body! [request reply]
  (js/Promise.
   (fn [resolve reject]
     (let [buffers (atom []) size (atom 0) stopped (atom false)]
       (.on request "data"
            (fn [chunk]
              (when-not @stopped
                (swap! size + (.-length chunk))
                (if (> @size (* 2 1024 1024))
                  (do (reset! stopped true) (reset! buffers [])
                      (reply 413 #js {:error "Webhook too large; use media download"})
                      (.resume request) (resolve nil))
                  (swap! buffers conj chunk)))))
       (.on request "end" #(when-not @stopped (resolve (js/Buffer.concat (into-array @buffers)))))
       (.on request "error" reject)
       (.on request "aborted" #(reject (js/Error. "Request aborted")))))))

(defn review! [{:keys [store]} body reply]
  (let [db (:db store)
        job (.get (.prepare db "SELECT * FROM jobs WHERE id=? AND state IN ('failed','uncertain')") (js/Number (.-job body)))]
    (cond
      (or (not job) (not (#{"retry" "skip"} (.-action body))))
      (reply 400 #js {:error "Choose retry or skip for an existing failed/uncertain job"})
      (and (= "uncertain" (.-state job)) (not (true? (.-confirmNoDelivery body))) (= "retry" (.-action body)))
      (reply 409 #js {:error "Inspect both chats first, then explicitly confirmNoDelivery to retry"})
      :else
      (do
        (if (= "retry" (.-action body))
          (let [prefix (js/String (.-dedup job)) payload (js/JSON.parse (.-payload job)) message (w/at payload "update" "message")]
            (.run (.prepare db "UPDATE effects SET state='pending' WHERE state='uncertain' AND (id=? OR substr(id,1,?)=?)")
                  prefix (inc (.-length prefix)) (str prefix ":"))
            (when (w/truthy? message)
              (.run (.prepare db "UPDATE effects SET state='pending' WHERE state='uncertain' AND id=?")
                    (str "send:" (when (= "private" (w/at message "chat" "type")) (str (.-bot payload) ":"))
                         (w/at message "chat" "id") ":" (.-message_id message))))
            (store/fail! store (js/Number (.-id job)) "pending" "Reviewed and retried"))
          (store/finish! store (js/Number (.-id job))))
        (reply 200 #js {:accepted true})))))

(defn- dispatch-body! [{:keys [config] :as dependencies} path bot raw reply]
  (if (and (= path "/webhooks/openwa")
           (not (config/equal? (:openwa-signature dependencies)
                               (str "sha256=" (config/sign (.toString raw) (:webhookSecret config))))))
    (reply 401 #js {:error "Invalid OpenWA signature"})
    (let [parsed (try #js {:body (js/JSON.parse (.toString raw))}
                      (catch :default _ #js {:invalid true}))
          body (.-body parsed)]
      (cond
        (.-invalid parsed) (reply 400 #js {:error "Invalid JSON"})
        (or (not (w/truthy? body)) (not (object? body)) (array? body)) (reply 400 #js {:error "Expected object"})
        (= path "/v1/review") (review! dependencies body reply)
        :else (reply 200 #js {:accepted (cond bot (ingress/accept-telegram! dependencies bot body)
                                             (= path "/webhooks/openwa") (ingress/accept-whatsapp! dependencies body)
                                             :else (ingress/accept-management! dependencies body))})))))

(defn handler [dependencies]
  (fn [request response]
    (let [{:keys [config]} dependencies
          reply (fn [status body]
                  (.writeHead response status #js {"content-type" "application/json"})
                  (.end response (js/JSON.stringify body)))
          process-request
          (fn [_]
            (let [path (.-pathname (js/URL. (.-url request) "http://bridge"))
                  telegram (.exec #"^/webhooks/telegram/([a-zA-Z0-9_-]+)$" path)
                  bot (when telegram (aget telegram 1))
                  headers (.-headers request)
                  admin (config/equal? (w/first-truthy (aget headers "authorization") "") (str "Bearer " (:admin config)))]
              (cond
                (and (= "GET" (.-method request)) (= path "/healthz"))
                (reply 200 #js {:status "ok" :configured (.-configured (ingress/status dependencies))})
                (and (= "GET" (.-method request)) (= path "/v1/status"))
                (if admin (reply 200 (ingress/status dependencies)) (reply 401 #js {:error "Unauthorized"}))
                (not= "POST" (.-method request)) (reply 404 #js {:error "Not found"})
                (and telegram (not (config/equal? (w/first-truthy (aget headers "x-telegram-bot-api-secret-token") "")
                                                  (routing/webhook-secret dependencies bot))))
                (reply 401 #js {:error "Invalid Telegram signature"})
                (and (= path "/v1/review") (not admin)) (reply 401 #js {:error "Unauthorized"})
                (and (not telegram) (not (#{"/webhooks/openwa" "/v1/management" "/v1/review"} path)))
                (reply 404 #js {:error "Not found"})
                :else (p/let [raw (read-body! request reply)]
                        (when raw
                          (dispatch-body! (assoc dependencies :openwa-signature (w/first-truthy (aget headers "x-openwa-signature") ""))
                                          path bot raw reply))))))]
      (-> (p/then (p/resolved nil) process-request)
          (p/catch (fn [error]
                     (let [data (api/error-data error)
                           status (if (and data (<= 400 (:status data)) (< (:status data) 600)) (:status data) 500)]
                       (reply status #js {:error (if data (ex-message error) "Internal error")}))))))))
