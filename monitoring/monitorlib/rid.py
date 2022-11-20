import datetime
from typing import Dict, List, Optional
import s2sphere

from implicitdict import ImplicitDict, StringBasedDateTime


MAX_SUB_PER_AREA = 10

MAX_SUB_TIME_HRS = 24

DATE_FORMAT = "%Y-%m-%dT%H:%M:%SZ"

ISA_PATH = "/v1/dss/identification_service_areas"
SUBSCRIPTION_PATH = "/v1/dss/subscriptions"
SCOPE_READ = "dss.read.identification_service_areas"
SCOPE_WRITE = "dss.write.identification_service_areas"
NetMaxNearRealTimeDataPeriod = datetime.timedelta(seconds=60)
NetMaxDisplayAreaDiagonal = 3.6  # km
NetDetailsMaxDisplayAreaDiagonal = 1.0  # km

# This scope is used only for experimentation during UPP2
UPP2_SCOPE_ENHANCED_DETAILS = "rid.read.enhanced_details"


def geo_polygon_string(vertices: List[Dict[str, float]]) -> str:
    return ",".join("{},{}".format(v["lat"], v["lng"]) for v in vertices)


def vertices_from_latlng_rect(rect: s2sphere.LatLngRect) -> List[Dict[str, float]]:
    return [
        {"lat": rect.lat_lo().degrees, "lng": rect.lng_lo().degrees},
        {"lat": rect.lat_lo().degrees, "lng": rect.lng_hi().degrees},
        {"lat": rect.lat_hi().degrees, "lng": rect.lng_hi().degrees},
        {"lat": rect.lat_hi().degrees, "lng": rect.lng_lo().degrees},
    ]


class ISA(dict):
    @property
    def errors(self) -> List[str]:
        errors: List[str] = []
        if "flights_url" not in self:
            errors.append("flights_url field missing")
        return errors

    @property
    def id(self) -> Optional[str]:
        return self.get("id", None)

    @property
    def owner(self) -> Optional[str]:
        return self.get("owner", None)

    @property
    def flights_url(self) -> Optional[str]:
        return self.get("flights_url", None)


class Flight(dict):
    @property
    def valid(self) -> bool:
        if self.id is None:
            return False
        return True

    @property
    def id(self) -> str:
        return self.get("id", None)


class FlightDetails(dict):
    pass


class Subscription(dict):
    @property
    def valid(self) -> bool:
        if self.version is None:
            return False
        return True

    @property
    def version(self) -> Optional[str]:
        return self.get("version", None)


# === Mirrors of types defined in ASTM remote ID standard ===


class ErrorResponse(ImplicitDict):
    message: Optional[str]


class LatLngPoint(ImplicitDict):
    lat: float
    lng: float


class RIDAuthData(ImplicitDict):
    format: str
    data: str


class RIDFlightDetails(ImplicitDict):
    id: str
    operator_id: Optional[str]
    operator_location: Optional[LatLngPoint]
    operation_description: Optional[str]
    auth_data: Optional[RIDAuthData]
    serial_number: Optional[str]
    registration_number: Optional[str]


class RIDAircraftPosition(ImplicitDict):
    lat: float
    lng: float
    alt: float
    accuracy_h: str
    accuracy_v: str
    extrapolated: Optional[bool]
    pressure_altitude: Optional[float]


class RIDHeight(ImplicitDict):
    distance: float
    reference: str


class RIDAircraftState(ImplicitDict):
    timestamp: StringBasedDateTime
    timestamp_accuracy: float
    operational_status: Optional[str]
    position: RIDAircraftPosition
    track: float
    speed: float
    speed_accuracy: str
    vertical_speed: float
    height: Optional[RIDHeight]


class RIDRecentAircraftPosition(ImplicitDict):
    time: StringBasedDateTime
    position: RIDAircraftPosition


class RIDFlight(ImplicitDict):
    id: str
    aircraft_type: str
    current_state: Optional[RIDAircraftState]
    # volumes: Optional[List[Volume4D]]
    simulated: Optional[bool]
    recent_positions: Optional[List[RIDRecentAircraftPosition]]


class GetFlightDetailsResponse(ImplicitDict):
    details: RIDFlightDetails


class GetFlightsResponse(ImplicitDict):
    timestamp: StringBasedDateTime
    flights: List[RIDFlight]


class IdentificationServiceArea(ImplicitDict):
    flights_url: str
    owner: str
    time_start: StringBasedDateTime
    time_end: StringBasedDateTime
    version: str
    id: str


class SubscriptionState(ImplicitDict):
    subscription_id: str
    notification_index: int = 0


class SubscriberToNotify(ImplicitDict):
    subscriptions: List[SubscriptionState] = []
    url: str
