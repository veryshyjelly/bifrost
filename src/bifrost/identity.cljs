(ns bifrost.identity
  (:require ["node:crypto" :as crypto]))

;; The persisted session fields contain this stable account, not the current
;; transport session. Do not re-key receipts when OpenWA rotates its session.
(defn account-identity!
  [{:keys [get-value put-value uuid]
    :or {uuid crypto/randomUUID}} account legacy-session]
  (let [saved (get-value "identity" "account")]
    (if (js/Boolean saved)
      (let [id (.-id saved)]
        (when (or (not (string? id)) (empty? id)
                  (and (js/Boolean account) (not= account id)))
          (throw (js/Error. "BIFROST_ACCOUNT_ID does not match the restored database")))
        id)
      (let [id (cond (js/Boolean account) account
                     (js/Boolean legacy-session) legacy-session
                     :else (uuid))]
        (put-value "identity" "account" #js {:id id})
        id))))
