"""Контур карты мира для радара: world-atlas 110m (TopoJSON, Natural Earth, public domain) → проекция Natural
Earth → пути SVG по странам с кодом ISO alpha-2, макрорегионом и центроидом.

    python3 -I scripts/world-map/build.py [countries-110m.json]

Без аргумента файл качается с unpkg (world-atlas@2.0.2). Результат — frontend/src/features/trend/worldMap.json.
Регион ставится по спискам ниже, остальное — по центроиду; код ISO есть у стран, которые отдаёт OpenAlex.
"""
import json
import math
import sys
import urllib.request
from pathlib import Path

SOURCE = "https://unpkg.com/world-atlas@2.0.2/countries-110m.json"
OUT = Path(__file__).resolve().parents[2] / "frontend/src/features/trend/worldMap.json"
W, H = 960, 480

NA = {"United States of America", "Canada", "Greenland", "Mexico"}
LATAM = {"Brazil", "Argentina", "Chile", "Colombia", "Peru", "Venezuela", "Ecuador", "Bolivia", "Paraguay", "Uruguay",
         "Guyana", "Suriname", "Cuba", "Haiti", "Dominican Rep.", "Jamaica", "Trinidad and Tobago", "Bahamas", "Belize",
         "Guatemala", "Honduras", "El Salvador", "Nicaragua", "Costa Rica", "Panama", "Puerto Rico", "Falkland Is.",
         "Fr. S. Antarctic Lands"}
EUROPE = {"United Kingdom", "Ireland", "France", "Spain", "Portugal", "Germany", "Netherlands", "Belgium", "Luxembourg",
          "Switzerland", "Austria", "Italy", "Denmark", "Norway", "Sweden", "Finland", "Iceland", "Poland", "Czechia",
          "Slovakia", "Hungary", "Romania", "Bulgaria", "Greece", "Croatia", "Slovenia", "Bosnia and Herz.", "Serbia",
          "Montenegro", "Albania", "North Macedonia", "Macedonia", "Kosovo", "Estonia", "Latvia", "Lithuania", "Moldova",
          "Ukraine", "Belarus", "Cyprus", "N. Cyprus", "Malta"}
MENA = {"Turkey", "Israel", "Palestine", "Lebanon", "Syria", "Jordan", "Iraq", "Iran", "Saudi Arabia", "Yemen", "Oman",
        "United Arab Emirates", "Qatar", "Kuwait", "Bahrain", "Egypt", "Libya", "Tunisia", "Algeria", "Morocco",
        "W. Sahara"}
CIS = {"Russia", "Kazakhstan", "Uzbekistan", "Turkmenistan", "Kyrgyzstan", "Tajikistan", "Armenia", "Azerbaijan",
       "Georgia"}
ISO = {"United States of America": "US", "Canada": "CA", "Mexico": "MX", "Brazil": "BR", "Argentina": "AR", "Chile": "CL",
       "Colombia": "CO", "Peru": "PE", "United Kingdom": "GB", "Ireland": "IE", "France": "FR", "Germany": "DE",
       "Netherlands": "NL", "Belgium": "BE", "Switzerland": "CH", "Austria": "AT", "Italy": "IT", "Spain": "ES",
       "Portugal": "PT", "Sweden": "SE", "Norway": "NO", "Denmark": "DK", "Finland": "FI", "Poland": "PL", "Czechia": "CZ",
       "Hungary": "HU", "Romania": "RO", "Greece": "GR", "Ukraine": "UA", "Russia": "RU", "Kazakhstan": "KZ",
       "Turkey": "TR", "Israel": "IL", "Saudi Arabia": "SA", "United Arab Emirates": "AE", "Qatar": "QA", "Egypt": "EG",
       "South Africa": "ZA", "Nigeria": "NG", "Kenya": "KE", "Morocco": "MA", "India": "IN", "Pakistan": "PK",
       "Bangladesh": "BD", "China": "CN", "Japan": "JP", "South Korea": "KR", "Taiwan": "TW", "Malaysia": "MY",
       "Indonesia": "ID", "Thailand": "TH", "Vietnam": "VN", "Philippines": "PH", "Australia": "AU", "New Zealand": "NZ",
       "Iran": "IR", "Iraq": "IQ", "Ethiopia": "ET", "Tanzania": "TZ", "Ghana": "GH", "Slovakia": "SK", "Croatia": "HR",
       "Serbia": "RS", "Bulgaria": "BG", "Lithuania": "LT", "Latvia": "LV", "Estonia": "EE", "Slovenia": "SI",
       "Luxembourg": "LU", "Cyprus": "CY", "Iceland": "IS", "Uzbekistan": "UZ", "Azerbaijan": "AZ", "Georgia": "GE",
       "Armenia": "AM", "Belarus": "BY", "Moldova": "MD", "Jordan": "JO", "Lebanon": "LB", "Kuwait": "KW", "Oman": "OM",
       "Tunisia": "TN", "Algeria": "DZ", "Sri Lanka": "LK", "Nepal": "NP", "Myanmar": "MM", "Cambodia": "KH",
       "Mongolia": "MN", "Uruguay": "UY", "Ecuador": "EC", "Bolivia": "BO", "Venezuela": "VE", "Costa Rica": "CR",
       "Panama": "PA", "Cuba": "CU", "Dominican Rep.": "DO", "Guatemala": "GT", "Syria": "SY", "Yemen": "YE",
       "Afghanistan": "AF", "Libya": "LY", "Sudan": "SD", "Uganda": "UG", "Cameroon": "CM", "Senegal": "SN",
       "Zimbabwe": "ZW", "Zambia": "ZM", "Mozambique": "MZ", "Angola": "AO", "Dem. Rep. Congo": "CD", "Rwanda": "RW",
       "Madagascar": "MG", "Mali": "ML", "Niger": "NE", "Burkina Faso": "BF", "Côte d'Ivoire": "CI", "Benin": "BJ",
       "Togo": "TG", "Botswana": "BW", "Namibia": "NA", "Malawi": "MW", "Somalia": "SO", "Laos": "LA", "Papua New Guinea": "PG",
       "North Korea": "KP", "Kyrgyzstan": "KG", "Tajikistan": "TJ", "Turkmenistan": "TM", "Paraguay": "PY", "Haiti": "HT",
       "Jamaica": "JM", "Honduras": "HN", "Nicaragua": "NI", "El Salvador": "SV", "Albania": "AL", "North Macedonia": "MK", "Macedonia": "MK",
       "Bosnia and Herz.": "BA", "Montenegro": "ME", "Kosovo": "XK", "Greenland": "GL", "Fiji": "FJ", "Bhutan": "BT",
       "Eritrea": "ER", "Chad": "TD", "Mauritania": "MR", "Guinea": "GN", "Sierra Leone": "SL", "Liberia": "LR",
       "Gabon": "GA", "Congo": "CG", "Central African Rep.": "CF", "S. Sudan": "SS", "Burundi": "BI", "Lesotho": "LS",
       "eSwatini": "SZ", "Djibouti": "DJ", "Timor-Leste": "TL", "Brunei": "BN", "Solomon Is.": "SB", "Vanuatu": "VU",
       "New Caledonia": "NC", "Trinidad and Tobago": "TT", "Bahamas": "BS", "Belize": "BZ", "Guyana": "GY",
       "Suriname": "SR", "Puerto Rico": "PR", "Palestine": "PS", "Guinea-Bissau": "GW", "Eq. Guinea": "GQ",
       "Gambia": "GM", "W. Sahara": "EH", "Falkland Is.": "FK", "Somaliland": "SO", "N. Cyprus": "CY"}
# Центроид по рамке контура уводит метку в океан у стран с заморскими территориями — задано руками.
CENTROID = {"United States of America": (205, 165), "Canada": (200, 110), "Russia": (700, 95), "China": (745, 180),
            "France": (470, 140), "Norway": (490, 95), "United Kingdom": (455, 118), "Japan": (835, 170)}
LABELS = {"americas": (150, 100), "latam": (300, 345), "europe": (372, 74), "cis": (805, 72), "mena": (520, 236),
          "africa": (500, 292), "asia": (862, 248)}


def project(lon, lat):
    lam, phi = math.radians(lon), math.radians(lat)
    p2 = phi * phi
    p4 = p2 * p2
    x = lam * (0.8707 - 0.131979 * p2 + p4 * (-0.013791 + p4 * (0.003971 * p2 - 0.001529 * p4)))
    y = phi * (1.007226 + p2 * (0.015085 + p4 * (-0.044475 + 0.028874 * p2 - 0.005916 * p4)))
    return x, -y


def region(name, lon, lat):
    for names, key in ((NA, "americas"), (LATAM, "latam"), (EUROPE, "europe"), (MENA, "mena"), (CIS, "cis")):
        if name in names:
            return key
    if -20 < lon < 55 and -40 < lat < 38:
        return "africa"
    if lon < -30 and lat < 15:
        return "latam"
    return "asia"


def main():
    if len(sys.argv) > 1:
        topo = json.loads(Path(sys.argv[1]).read_text())
    else:
        with urllib.request.urlopen(SOURCE, timeout=60) as response:  # noqa: S310 — фиксированный адрес
            topo = json.load(response)
    scale, translate = topo["transform"]["scale"], topo["transform"]["translate"]
    arcs = []
    for arc in topo["arcs"]:
        x = y = 0
        points = []
        for dx, dy in arc:
            x += dx
            y += dy
            points.append((x * scale[0] + translate[0], y * scale[1] + translate[1]))
        arcs.append(points)

    def ring(indices):
        out = []
        for i in indices:
            points = arcs[i] if i >= 0 else list(reversed(arcs[~i]))
            out.extend(points[1:] if out else points)
        return out

    countries = []
    for geometry in topo["objects"]["countries"]["geometries"]:
        name = geometry["properties"]["name"]
        if name == "Antarctica":
            continue
        polygons = geometry["arcs"] if geometry["type"] == "MultiPolygon" else [geometry["arcs"]]
        rings = []
        lon_sum = lat_sum = n = 0
        for polygon in polygons:
            points = ring(polygon[0])
            for lon, lat in points:
                lon_sum += lon
                lat_sum += lat
                n += 1
            # Разрыв по антимеридиану: скачок долготы больше 180° — новый подконтур, а не черта через карту.
            piece, previous = [], None
            for lon, lat in points:
                if previous is not None and abs(lon - previous) > 180:
                    rings.append(piece)
                    piece = []
                piece.append(project(lon, lat))
                previous = lon
            rings.append(piece)
        countries.append((name, region(name, lon_sum / n, lat_sum / n), rings))
    xs = [x for _, _, rings in countries for r in rings for x, _ in r]
    ys = [y for _, _, rings in countries for r in rings for _, y in r]
    k = min(W / (max(xs) - min(xs)), H / (max(ys) - min(ys))) * 0.98
    ox = (W - (max(xs) - min(xs)) * k) / 2 - min(xs) * k
    oy = (H - (max(ys) - min(ys)) * k) / 2 - min(ys) * k
    out = []
    for name, reg, rings in countries:
        d = " ".join("M" + " L".join(f"{x * k + ox:.1f},{y * k + oy:.1f}" for x, y in r) + "Z" for r in rings)
        px = [x * k + ox for r in rings for x, _ in r]
        py = [y * k + oy for r in rings for _, y in r]
        cx, cy = CENTROID.get(name, (sum(px) / len(px), sum(py) / len(py)))
        out.append({"n": name, "c": ISO.get(name), "r": reg, "x": round(cx), "y": round(cy), "d": d})
    OUT.write_text(json.dumps({"width": W, "height": H, "labels": LABELS, "countries": out}, ensure_ascii=False,
                              separators=(",", ":")) + "\n")
    print(len(out), "countries,", OUT.stat().st_size // 1024, "KB →", OUT)


if __name__ == "__main__":
    main()
