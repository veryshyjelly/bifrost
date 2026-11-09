(ns bifrost.wire
  (:refer-clojure :exclude [get]))

;; Keep JS omission semantics explicit at provider/storage boundaries. Clojure
;; nil is JSON null; an absent property must remain JS undefined instead.
(defn get [value key]
  (if (nil? value) js/undefined (aget value key)))

(defn at [value & keys] (reduce get value keys))
(defn truthy? [value] (js/Boolean value))
(defn first-truthy [& values]
  (loop [values values]
    (let [value (first values)]
      (if (or (truthy? value) (not (next values))) value (recur (next values))))))
(defn object [& parts] (apply js/Object.assign #js {} parts))
(defn number-or-zero [value] (js/Number (first-truthy value 0)))
