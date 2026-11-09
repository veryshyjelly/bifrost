(ns bifrost.effects
  (:require [bifrost.api :as api]
            [promesa.core :as p]))

(defn run! [{:keys [db]} id action]
  (let [effect (.get (.prepare db "SELECT * FROM effects WHERE id=?") id)
        state (some-> effect .-state)]
    (cond
      (= state "done") (p/resolved (js/JSON.parse (.-result effect)))
      (#{"uncertain" "sending"} state)
      (p/rejected (api/api-error "Uncertain external delivery; review before retrying" 0 false true 5000))
      :else
      (do
        (.run (.prepare db "INSERT INTO effects(id,state) VALUES(?,'sending') ON CONFLICT(id) DO UPDATE SET state='sending'") id)
        (-> (p/then (p/resolved nil) (fn [_] (action)))
            (p/then (fn [result]
                      (.run (.prepare db "UPDATE effects SET state='done',result=? WHERE id=?")
                            (js/JSON.stringify (if (nil? result) nil result)) id)
                      result))
            (p/catch (fn [error]
                       (let [classification (api/error-data error)]
                         (.run (.prepare db "UPDATE effects SET state=? WHERE id=?")
                               (if (and classification (not (:uncertain classification))) "pending" "uncertain") id))
                       (throw error))))))))
