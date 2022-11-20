import uuid
from typing import Tuple, List
from urllib.parse import urlparse

from implicitdict import ImplicitDict

from monitoring.monitorlib import infrastructure, fetch
from monitoring.monitorlib.clients.scd_automated_testing import (
    clear_area,
    create_flight,
    delete_flight,
    get_version,
    get_capabilities,
)
from monitoring.monitorlib.scd import Volume4D
from monitoring.monitorlib.scd_automated_testing.scd_injection_api import (
    InjectFlightResult,
    DeleteFlightResult,
    InjectFlightResponse,
    DeleteFlightResponse,
    InjectFlightRequest,
    Capability,
    ClearAreaResponse,
    ClearAreaRequest,
)


class FlightPlannerConfiguration(ImplicitDict):
    participant_id: str
    """ID of the flight planner into which test data can be injected"""

    injection_base_url: str
    """Base URL for the flight planner's implementation of the interfaces/automated-testing/scd/scd.yaml API"""

    def __init__(self, *args, **kwargs):
        super().__init__(**kwargs)
        try:
            urlparse(self.injection_base_url)
        except ValueError:
            raise ValueError(
                "FlightPlannerConfiguration.injection_base_url must be a URL"
            )


class FlightPlannerInformation(ImplicitDict):
    version: str
    capabilities: List[Capability]
    version_query: fetch.Query
    capabilities_query: fetch.Query


class FlightPlanner:
    """Manages the state and the interactions with flight planner USS"""

    def __init__(
        self,
        config: FlightPlannerConfiguration,
        auth_adapter: infrastructure.AuthAdapter,
    ):
        self.config = config
        self.client = infrastructure.UTMClientSession(
            self.config.injection_base_url, auth_adapter
        )

        # Flights injected by this target.
        self.created_flight_ids: List[str] = []

    def __repr__(self):
        return "FlightPlanner({}, {})".format(
            self.config.participant_id, self.config.injection_base_url
        )

    @property
    def name(self) -> str:
        return self.config.participant_id

    @property
    def participant_id(self):
        return self.config.participant_id

    def request_flight(
        self, request: InjectFlightRequest
    ) -> Tuple[InjectFlightResponse, fetch.Query, str]:
        flight_id, resp, query = create_flight(
            self.client, self.config.injection_base_url, request
        )
        if resp.result == InjectFlightResult.Planned:
            self.created_flight_ids.append(flight_id)

        return resp, query, flight_id

    def cleanup_flight(
        self, flight_id: str
    ) -> Tuple[DeleteFlightResponse, fetch.Query]:
        resp, query = delete_flight(
            self.client, self.config.injection_base_url, flight_id
        )
        if resp is not None and resp.result == DeleteFlightResult.Closed:
            self.created_flight_ids.remove(flight_id)
        return resp, query

    def get_target_information(self) -> FlightPlannerInformation:
        resp, version_query = get_version(self.client, self.config.injection_base_url)
        version = resp.version if resp.version is not None else "Unknown"
        resp, capabilities_query = get_capabilities(
            self.client, self.config.injection_base_url
        )

        return FlightPlannerInformation(
            version=version,
            capabilities=resp.capabilities,
            version_query=version_query,
            capabilities_query=capabilities_query,
        )

    def clear_area(self, extent: Volume4D) -> Tuple[ClearAreaResponse, fetch.Query]:
        req = ClearAreaRequest(request_id=str(uuid.uuid4()), extent=extent)
        return clear_area(self.client, self.config.injection_base_url, req)
