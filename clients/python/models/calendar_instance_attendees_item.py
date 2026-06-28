from __future__ import annotations

from collections.abc import Mapping
from typing import Any, TypeVar, BinaryIO, TextIO, TYPE_CHECKING, Generator

from attrs import define as _attrs_define
from attrs import field as _attrs_field

from ..types import UNSET, Unset

from ..types import UNSET, Unset






T = TypeVar("T", bound="CalendarInstanceAttendeesItem")



@_attrs_define
class CalendarInstanceAttendeesItem:
    """ 
        Attributes:
            email (str): The attendee's email address (from Google).
            person_id (str | Unset): The matched directory person id, when the email reconciles to a known person.
            response_status (str | Unset): Google RSVP status — needsAction, declined, tentative, or accepted.
     """

    email: str
    person_id: str | Unset = UNSET
    response_status: str | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        email = self.email

        person_id = self.person_id

        response_status = self.response_status


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "email": email,
        })
        if person_id is not UNSET:
            field_dict["person_id"] = person_id
        if response_status is not UNSET:
            field_dict["response_status"] = response_status

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        email = d.pop("email")

        person_id = d.pop("person_id", UNSET)

        response_status = d.pop("response_status", UNSET)

        calendar_instance_attendees_item = cls(
            email=email,
            person_id=person_id,
            response_status=response_status,
        )


        calendar_instance_attendees_item.additional_properties = d
        return calendar_instance_attendees_item

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
