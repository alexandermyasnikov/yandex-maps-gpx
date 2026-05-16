# 🗺️ Yandex Maps → GPX / GeoJSON Converter

Инструмент для конвертации закладок Yandex Maps (public bookmarks) в:
* GeoJSON (основной формат)
* с поддержкой кеширования
* с разрешением координат через Yandex Geocoder API



## 🎯 Зачем это нужно

Я использую Яндекс Карты как основной инструмент для поиска мест и хранения закладок.  
Все найденные точки сортирую по тематическим спискам: «Посещённые места», «Хочу посетить», «Рестораны» и т.д.

Перед отпуском или поездкой я экспортирую эти списки через эту программу и сохраняю в iCloud.  
На телефоне остаётся только импортировать их в офлайн-навигаторы — Organic Maps или OsmAnd.  
Так все мои сохранённые локации всегда под рукой, даже без интернета.



## 📌 Возможности

* 📥 Загрузка публичных bookmark-списков Yandex Maps
* 🧩 Парсинг HTML ответов (встроенный JSON)
* 📍 Поддержка разных типов ссылок:
  * `ymapsbm1://pin`
  * `ymapsbm1://org`
  * `ymapsbm1://geo`
* 🌍 Автоматическое получение координат через Geocoder API
* 💾 Persistent cache (URI + HTML ответы)
* ⚡ Cache-first стратегия (без повторных запросов)
* 🧾 Экспорт в GeoJSON (совместим с GIS инструментами)
* 🧠 Минимальные зависимости



## 🔑 API Key

Получите ключ на [Яндекс.Разработчики](https://developer.tech.yandex.ru/services)



## 📂 Идентификатор списка

1. Откройте [yandex.ru/maps](https://yandex.ru/maps)
2. Перейдите в профиль → «Закладки и мой транспорт»
3. У выбранного списка нажмите на многоточие → «Поделиться»
4. Скопируйте publicId списка из URL



## 🚀 Запуск

```bash
export YANDEX_GEOCODER_API_KEY=xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx

go run ./cmd/main.go -public-ids "xxxxxxxx,yyyyyyyy" -log-level debug
```

Где `xxxxxxxx,yyyyyyyy` — это списки публичных ID-шников bookmark-списков Yandex Maps.

### Параметры запуска
```bash
❯ go run ./cmd/main.go --help

Usage of /tmp/main:
  -api-key string
    	API key for Yandex Maps API (or use environment variable YANDEX_GEOCODER_API_KEY)
  -cache-dir string
    	directory for persistent cache (default "/tmp")
  -log-level value
    	log level (default WARN)
  -output-dir string
    	output file (default "/tmp")
  -public-ids string
    	comma-separated list of public IDs to convert
```



## 📄 Формат вывода

Каждый bookmark-список представлен в формате [GeoJSON](https://geojson.org/).
В имени файла есть номер ревизии для удобства отслеживания изменений.

Пример вывода:
```json
❯ cat /tmp/todo.ya-rev4472.geojson  | jq | head -n20

{
  "type": "FeatureCollection",
  "features": [
    {
      "type": "Feature",
      "geometry": {
        "type": "Point",
        "coordinates": [
          30.308913,
          59.952046
        ]
      },
      "properties": {
        "Text": "Россия, Северо-Западный федеральный округ, Санкт-Петербург, Санкт-Петербург, Петроградский район, муниципальный округ Кронверкское, Ленинградский зоопарк",
        "description": "Александровский парк, Санкт-Петербург, 1АИ",
        "marker-color": "racing:#50ba3d",
        "name": "Ленинградский зоопарк"
      }
    },
```



## ⚠️ Ограничения

* Yandex Maps не предоставляет официальное API для bookmark-списков
* HTML структура страниц может измениться
* При большом количестве запросов возможна CAPTCHA
* Geocoder API имеет ограничение в 1000 запросов в сутки на бесплатном тарифе
