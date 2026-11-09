(ns bifrost.runtime)

(def bun-version "1.4.2")

(defn assert-bun! []
  (when-not (= bun-version (some-> js/globalThis.Bun .-version))
    (throw (js/Error. (str "Bifrost requires Bun " bun-version))))
  true)
