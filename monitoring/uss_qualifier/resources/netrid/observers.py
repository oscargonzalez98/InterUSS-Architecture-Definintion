import datetime
from typing import List, Optional, Tuple

import s2sphere
from implicitdict import ImplicitDict

from monitoring.monitorlib import fetch, infrastructure
from monitoring.monitorlib.infrastructure import UTMClientSession
from monitoring.monitorlib.rid_common import RIDVersion
from monitoring.monitorlib.rid_automated_testing import observation_api
from monitoring.uss_qualifier.resources.resource import Resource
from monitoring.uss_qualifier.resources.communications import AuthAdapterResource


class RIDSystemObserver(object):
    def __init__(
        self,
        participant_id: str,
        base_url: str,
        auth_adapter: infrastructure.AuthAdapter,
    ):
        self.session = UTMClientSession(base_url, auth_adapter)
        self.participant_id = participant_id

        # TODO: Change observation API to use an InterUSS scope rather than re-using an ASTM scope
        self.rid_version = RIDVersion.f3411_19

    def observe_system(
        self, rect: s2sphere.LatLngRect
    ) -> Tuple[Optional[observation_api.GetDisplayDataResponse], fetch.Query]:
        initiated_at = datetime.datetime.utcnow()
        resp = self.session.get(
            "/display_data?view={},{},{},{}".format(
                rect.lo().lat().degrees,
                rect.lo().lng().degrees,
                rect.hi().lat().degrees,
                rect.hi().lng().degrees,
            ),
            scope=self.rid_version.read_scope,
        )
        try:
            result = (
                ImplicitDict.parse(resp.json(), observation_api.GetDisplayDataResponse)
                if resp.status_code == 200
                else None
            )
        except ValueError as e:
            print("Error parsing observation response: {}".format(e))
            result = None
        return (result, fetch.describe_query(resp, initiated_at))

    def observe_flight_details(
        self, flight_id: str
    ) -> Tuple[Optional[observation_api.GetDetailsResponse], fetch.Query]:
        initiated_at = datetime.datetime.utcnow()
        resp = self.session.get("/display_data/{}".format(flight_id))
        try:
            result = (
                ImplicitDict.parse(resp.json(), observation_api.GetDetailsResponse)
                if resp.status_code == 200
                else None
            )
        except ValueError:
            result = None
        return (result, fetch.describe_query(resp, initiated_at))


class ObserverConfiguration(ImplicitDict):
    participant_id: str
    """Participant ID of the observer providing a view of RID data in the system"""

    observation_base_url: str
    """Base URL for the observer's implementation of the interfaces/automated-testing/rid/observation.yaml API"""


class NetRIDObserversSpecification(ImplicitDict):
    observers: List[ObserverConfiguration]


class NetRIDObserversResource(Resource[NetRIDObserversSpecification]):
    observers: List[RIDSystemObserver]

    def __init__(
        self,
        specification: NetRIDObserversSpecification,
        auth_adapter: AuthAdapterResource,
    ):
        self.observers = [
            RIDSystemObserver(
                o.participant_id, o.observation_base_url, auth_adapter.adapter
            )
            for o in specification.observers
        ]
