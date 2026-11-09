(ns bifrost.worker
  (:require [bifrost.store :as store]
            [bifrost.api :as api]
            [bifrost.ingress :as ingress]
            [bifrost.relay :as relay]
            [bifrost.text :as text]
            [bifrost.wire :as w]
            [promesa.core :as p]))

(defn manage! [{:keys [config api] :as dependencies} event key]
  (let [data (.-data event)]
    (case (.-type event)
      "mark_read" (p/let [result ((:ow api) "chats/read" #js {:chatId (.-chatId data)})]
                    (when (false? (.-success result))
                      (throw (api/api-error "OpenWA declined the read receipt; mark read in WhatsApp" 409 false false 5000))))
      ("session_status" "delivery_problem")
      (text/send! dependencies "master" (w/first-truthy (:management config) (:owner config))
                  (if (= "delivery_problem" (.-type event))
                    (str "Delivery job " (.-job data) " is " (.-state data) ": " (w/first-truthy (.-error data) "review required") ". Use /status. Review it with the local bridge CLI before retrying.")
                    (str "OpenWA: " (.-event data) " · " (.-status data))) key)
      ("return_to_inbox" "sync_metadata" "link_group" "new_conversation" "operator_action")
      (relay/workflow! dependencies :management event key)
      (p/rejected (api/api-error "Unknown management action" 400 false false 5000)))))

(defn- dispatch! [{:keys [store] :as dependencies} job]
  (let [type (.-type job) payload (.-payload job) key (.-dedup job)]
    (cond
      (= "wa" type) (relay/deliver-whatsapp! dependencies payload key)
      (= "tg" type) (relay/telegram! dependencies (.-bot payload) (.-update payload) key)
      (= "management" type) (manage! dependencies payload key)
      (= "history" type) (relay/workflow! dependencies :history-fetch payload key)
      (#{"history-message" "history-finish"} type) (relay/workflow! dependencies :history-job job)
      (.startsWith type "control-") (relay/workflow! dependencies :control-job job)
      (.startsWith type "sync-") (relay/workflow! dependencies :sync-job job)
      (.startsWith type "status-") (relay/workflow! dependencies :status-job job)
      (= "n8n" type) (do (store/enqueue! store (str "management:" (js/String (.-id payload))) (str "management:" (js/String (.-id payload))) "management" payload)
                         (p/resolved js/undefined))
      :else (p/resolved js/undefined))))

(defn- failed! [{:keys [store log-error] :or {log-error js/console.error} :as dependencies} job error]
  (let [classification (or (api/error-data error) {:status 0 :retry false :uncertain true :delay 5000})
        message (if (api/error-data error) (ex-message error) "Internal processing error; inspect local service logs")
        state (cond (w/truthy? (:uncertain classification)) "uncertain"
                    (and (w/truthy? (:retry classification)) (< (.-attempts job) 8)) "pending" :else "failed")
        type (.-type job)]
    (store/fail! store (.-id job) state message (js/Math.max (:delay classification) (js/Math.min 300000 (* 1000 (js/Math.pow 2 (.-attempts job))))))
    (when (and (not= state "pending") (or (= "history" type) (.startsWith type "history-")))
      (when-let [run (when (w/truthy? (w/at job "payload" "run")) (store/get store "history-run" (w/at job "payload" "run")))]
        (when (not= "complete" (.-status run)) (store/put! store "history-run" (.-id run) (w/object run #js {:status "failed"})))))
    (when (and (#{"sync-start" "sync-finish"} type) (not= state "pending"))
      (when-let [run (store/get store "sync-run" (w/at job "payload" "run"))]
        (store/put! store "sync-run" (w/at job "payload" "run") (w/object run #js {:status "failed" :error message}))))
    (log-error (js/JSON.stringify #js {:event "job_failed" :job (.-id job) :state state :error message}))
    (when (and (not= type "n8n") (not (.startsWith type "control-"))
               (not (and (= type "management") (#{"delivery_problem" "session_status"} (w/at job "payload" "type")))))
      (ingress/management! dependencies "delivery_problem" #js {:job (.-id job) :state state :error message}
                           (str "failure:" (.-id job) ":" (.-attempts job))))))

(defn tick! [{:keys [store busy] :as dependencies}]
  (if @busy (p/resolved js/undefined)
      (do
        (reset! busy true)
        (-> (p/then (p/resolved nil)
                    (fn [_]
                      (when-let [job (store/next! store)]
                        (-> (p/then (p/resolved nil) (fn [_] (dispatch! dependencies job)))
                            (p/then (fn [_] (store/finish! store (.-id job)) js/undefined))
                            (p/catch (fn [error] (failed! dependencies job error)))))))
            (p/finally #(reset! busy false))))))
