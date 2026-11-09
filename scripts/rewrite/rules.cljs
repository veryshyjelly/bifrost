;; Offline comparison boundary: JSON in, JSON out, no provider dependencies.
(ns rewrite-rules
  (:require ["node:fs" :as fs]
            [bifrost.runtime :as runtime]
            [bifrost.format :as format]
            [bifrost.config :as config]
            [bifrost.cron :as cron]))

(runtime/assert-bun!)
(def input (js/JSON.parse (fs/readFileSync 0 "utf8")))
(defn run [row]
  (let [{:keys [op args now]} (js->clj row :keywordize-keys true)]
    (try
      {:value (case op
                "plain" (format/plain (first args))
                "styled" (apply format/styled args)
                "concat" (apply format/concat args)
                "whatsappURL" (apply format/whatsapp-url args)
                "senderName" (apply format/sender-name args)
                "historyTime" (format/history-time (first args) (or (second args) "History") now)
                "historyClock" (format/history-clock (first args) (or (second args) "UTC") now)
                "historyDay" (format/history-day (first args) (second args) now)
                "relayHeader" (format/relay-header (first args) now)
                "relayText" (format/relay-text (first args) (second args) now)
                "sliceFormatted" (apply format/slice-formatted args)
                "splitFormatted" (apply format/split-formatted args)
                "captionFields" (apply format/caption-fields args)
                "configuration" (config/configuration (aget (.-args row) 0))
                "validate" (do (config/validate! (config/configuration (aget (.-args row) 0))) true)
                "cron" (let [schedule (cron/schedule (first args) (second args))]
                         (mapv #(schedule (js/Date. %)) (nth args 2)))
                "digest" (apply config/digest args)
                "sign" (apply config/sign args)
                "equal" (apply config/equal? args))}
      (catch :default error {:error (.-message error)}))))

(println (js/JSON.stringify (clj->js (mapv run input))))
