(ns rewrite-runtime
  (:require [nbb.core :as nbb]
            ["node:fs" :as fs]
            ["node:os" :as os]
            ["node:path" :as path]
            ["node:http" :as http]
            ["node:child_process" :as child]
            ["node:module" :as module]
            ["node:url" :as url]
            ["puppeteer-core" :as puppeteer]
            [bifrost.runtime :as runtime]
            [bifrost.api :as api]
            [bifrost.store :as store]
            [promesa.core :as p]))

(runtime/assert-bun!)
(def resource-file nbb/*file*)
(def directory (fs/mkdtempSync (path/join (os/tmpdir) "bifrost-nbb-runtime-")))
(def filename (path/join directory "fixture.bin"))
(def content (js/Buffer.from "synthetic UTF-16 😀\n"))
(fs/writeFileSync filename content #js {:mode 384})

(defn collect [stream]
  (js/Promise. (fn [resolve reject]
                 (let [chunks (atom [])]
                   (.on stream "data" #(swap! chunks conj %))
                   (.on stream "error" reject)
                   (.on stream "end" #(resolve (js/Buffer.concat (into-array @chunks))))))))

(def server
  (http/createServer
   (fn [request response]
     (-> (p/let [body (collect request)]
           (.writeHead response 200 #js {"content-type" "application/json"})
           (.end response (js/JSON.stringify #js {:ok true :result #js {:method (.-method request)
                                                                      :contentType (aget (.-headers request) "content-type")
                                                                      :body (.toString body "base64")}})))
         (p/catch (fn [_] (.writeHead response 500) (.end response "probe failure")))))))

(defn check-files! []
  (let [link (path/join directory "inside-link")
        require (module/createRequire (url/pathToFileURL resource-file))
        lottie (.resolve require "lottie-web/build/player/lottie_light.min.js")]
    (fs/symlinkSync filename link)
    (assert (= (fs/realpathSync filename) (fs/realpathSync link)))
    (assert (> (.-length (fs/readFileSync lottie)) 0))
    (assert (fn? puppeteer/launch))
    (assert (= 384 (bit-and (.-mode (fs/statSync filename)) 511)))))

(defn check-worker! []
  (let [result (child/execFileSync js/process.execPath
                                  #js [(path/resolve "node_modules/nbb/cli.js") "-cp" (path/resolve "src")
                                       "-e" "(require '[bifrost.runtime :as r]) (r/assert-bun!) (println js/Bun.version)"]
                                  #js {:encoding "utf8" :timeout 10000})]
    (assert (= (str runtime/bun-version "\n") result))))

(-> (p/let [_ (js/Promise. (fn [resolve reject]
                            (.once server "error" reject)
                            (.listen server 0 "127.0.0.1" resolve)))
            _ (check-files!)
            _ (check-worker!)
            streamed (collect (fs/createReadStream filename))
            _ (assert (.equals content streamed))
            adapters (api/adapters {:telegram (str "http://127.0.0.1:" (.-port (.address server)))})
            json ((:tg adapters) "synthetic-token" "sendMessage" #js {:text "😀" :entities #js [] :missing js/undefined :present nil})
            _ (assert (= "{\"text\":\"😀\",\"entities\":[],\"present\":null}" (.toString (js/Buffer.from (.-body json) "base64"))))
            multipart ((:upload adapters) "synthetic-token" "sendDocument" "document" filename "fixture.bin"
                       #js {:caption "😀" :caption_entities #js [#js {:type "bold" :offset 0 :length 2}]})
            packet (js/Request. "http://fixture" #js {:method "POST" :headers #js {"content-type" (.-contentType multipart)}
                                                     :body (js/Buffer.from (.-body multipart) "base64")})
            form (.formData packet)
            file (.get form "document")
            bytes (.arrayBuffer file)
            _ (assert (.equals content (js/Buffer.from bytes)))
            _ (assert (= "😀" (.get form "caption")))
            _ (assert (= "[{\"type\":\"bold\",\"offset\":0,\"length\":2}]" (.get form "caption_entities")))
            database (store/open! (path/join directory "synthetic.db") (.repeat "aa" 32))]
      (try
        (assert (= "synthetic" (store/unseal database (store/seal database "synthetic"))))
        (finally (store/close! database)))
      (println (js/JSON.stringify #js {:bun js/Bun.version :platform js/process.platform :architecture js/process.arch
                                      :sqlite true :aesGcm true :fetch true :streams true :multipart true
                                      :openAsBlob true :filesystem true :npmResources true :workerBun true})))
    (p/finally (fn [] (.close server) (fs/rmSync directory #js {:recursive true :force true}))))
