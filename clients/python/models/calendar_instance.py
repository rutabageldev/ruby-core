from __future__ import annotations

from collections.abc import Mapping
from typing import Any, TypeVar, BinaryIO, TextIO, TYPE_CHECKING, Generator

from attrs import define as _attrs_define
from attrs import field as _attrs_field

from ..types import UNSET, Unset

from ..types import UNSET, Unset
from typing import cast
import datetime

if TYPE_CHECKING:
  from ..models.calendar_instance_attendees_item import CalendarInstanceAttendeesItem





T = TypeVar("T", bound="CalendarInstance")



@_attrs_define
class CalendarInstance:
    """ One concrete occurrence of a calendar event in the requested window. Recurring
    series are expanded into instances server-side, timezone-aware. Times are RFC 3339
    UTC instants; for all-day events `all_day` is true and `end` is EXCLUSIVE (a
    one-day event on the 26th ends on the 27th — surfaced, not hidden; see ADR-0042).
    `subjects` and `childcare` are resolved from the local overlay (populated in the
    household-overlay slice).

        Example:
            {'google_event_id': 'abc123', 'summary': 'Dentist', 'start': '2026-06-26T13:00:00Z', 'end':
                '2026-06-26T14:00:00Z', 'all_day': False, 'status': 'confirmed', 'location': '123 Main St', 'description':
                'Cleaning', 'subjects': [], 'attendees': [{'email': 'michael.katie.rubanka@gmail.com', 'person_id':
                '11111111-1111-4111-8111-111111111111', 'response_status': 'accepted'}]}

        Attributes:
            google_event_id (str): The Google event id of the master (or override child) this instance came from.
            start (datetime.datetime): Instance start as an RFC 3339 UTC instant.
            end (datetime.datetime): Instance end as an RFC 3339 UTC instant (exclusive for all-day events).
            all_day (bool): Whether this is an all-day event (date-only in Google).
            status (str): Google event status — confirmed, tentative, or cancelled.
            summary (str | Unset): The event title.
            location (str | Unset): Free-text location, when set.
            description (str | Unset): Free-text description, when set.
            subjects (list[str] | Unset): Resolved subject person ids (the "FOR" lane) — local overlay, never written to
                Google.
            childcare (str | Unset): Resolved childcare provider id for this event; omitted when none — local overlay.
            attendees (list[CalendarInstanceAttendeesItem] | Unset): Google attendees, each reconciled to a directory person
                id by email where matched, with Google RSVP status.
     """

    google_event_id: str
    start: datetime.datetime
    end: datetime.datetime
    all_day: bool
    status: str
    summary: str | Unset = UNSET
    location: str | Unset = UNSET
    description: str | Unset = UNSET
    subjects: list[str] | Unset = UNSET
    childcare: str | Unset = UNSET
    attendees: list[CalendarInstanceAttendeesItem] | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.calendar_instance_attendees_item import CalendarInstanceAttendeesItem
        google_event_id = self.google_event_id

        start = self.start.isoformat()

        end = self.end.isoformat()

        all_day = self.all_day

        status = self.status

        summary = self.summary

        location = self.location

        description = self.description

        subjects: list[str] | Unset = UNSET
        if not isinstance(self.subjects, Unset):
            subjects = self.subjects



        childcare = self.childcare

        attendees: list[dict[str, Any]] | Unset = UNSET
        if not isinstance(self.attendees, Unset):
            attendees = []
            for attendees_item_data in self.attendees:
                attendees_item = attendees_item_data.to_dict()
                attendees.append(attendees_item)




        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "google_event_id": google_event_id,
            "start": start,
            "end": end,
            "all_day": all_day,
            "status": status,
        })
        if summary is not UNSET:
            field_dict["summary"] = summary
        if location is not UNSET:
            field_dict["location"] = location
        if description is not UNSET:
            field_dict["description"] = description
        if subjects is not UNSET:
            field_dict["subjects"] = subjects
        if childcare is not UNSET:
            field_dict["childcare"] = childcare
        if attendees is not UNSET:
            field_dict["attendees"] = attendees

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.calendar_instance_attendees_item import CalendarInstanceAttendeesItem
        d = dict(src_dict)
        google_event_id = d.pop("google_event_id")

        start = datetime.datetime.fromisoformat(d.pop("start"))




        end = datetime.datetime.fromisoformat(d.pop("end"))




        all_day = d.pop("all_day")

        status = d.pop("status")

        summary = d.pop("summary", UNSET)

        location = d.pop("location", UNSET)

        description = d.pop("description", UNSET)

        subjects = cast(list[str], d.pop("subjects", UNSET))


        childcare = d.pop("childcare", UNSET)

        _attendees = d.pop("attendees", UNSET)
        attendees: list[CalendarInstanceAttendeesItem] | Unset = UNSET
        if _attendees is not UNSET:
            attendees = []
            for attendees_item_data in _attendees:
                attendees_item = CalendarInstanceAttendeesItem.from_dict(attendees_item_data)



                attendees.append(attendees_item)


        calendar_instance = cls(
            google_event_id=google_event_id,
            start=start,
            end=end,
            all_day=all_day,
            status=status,
            summary=summary,
            location=location,
            description=description,
            subjects=subjects,
            childcare=childcare,
            attendees=attendees,
        )


        calendar_instance.additional_properties = d
        return calendar_instance

    @property
    def additional_keys(self) -> list[str]:
        return list(self.additional_properties.keys())

    def __getitem__(self, key: str) -> Any:
        return self.additional_properties[key]

    def __setitem__(self, key: str, value: Any) -> None:
        self.additional_properties[key] = value

    def __delitem__(self, key: str) -> None:
        del self.additional_properties[key]

    def __contains__(self, key: str) -> bool:
        return key in self.additional_properties
