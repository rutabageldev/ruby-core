from __future__ import annotations

from collections.abc import Mapping
from typing import Any, TypeVar, BinaryIO, TextIO, TYPE_CHECKING, Generator

from attrs import define as _attrs_define
from attrs import field as _attrs_field

from ..types import UNSET, Unset

from ..types import UNSET, Unset






T = TypeVar("T", bound="Problem")



@_attrs_define
class Problem:
    """ RFC 9457 Problem Details. The single error shape returned by every operation in
    this API (ADR-0041). Domain-specific context is carried in extension members rather
    than by string-munging `detail`.

        Example:
            {'type': 'about:blank', 'title': 'Bad Request', 'status': 400, 'detail': 'The requested date range exceeds the
                maximum allowed window.', 'instance': '/v1/calendar/events'}

        Attributes:
            title (str): A short, human-readable, stable summary of the problem type.
            status (int): The HTTP status code, duplicated here for clients that read the body.
            type_ (str | Unset): A URI reference identifying the problem type. Defaults to `about:blank` when no specific
                type applies.
            detail (str | Unset): A human-readable explanation specific to this occurrence of the problem.
            instance (str | Unset): A URI reference identifying the specific occurrence (typically the request path).
     """

    title: str
    status: int
    type_: str | Unset = UNSET
    detail: str | Unset = UNSET
    instance: str | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        title = self.title

        status = self.status

        type_ = self.type_

        detail = self.detail

        instance = self.instance


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "title": title,
            "status": status,
        })
        if type_ is not UNSET:
            field_dict["type"] = type_
        if detail is not UNSET:
            field_dict["detail"] = detail
        if instance is not UNSET:
            field_dict["instance"] = instance

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        title = d.pop("title")

        status = d.pop("status")

        type_ = d.pop("type", UNSET)

        detail = d.pop("detail", UNSET)

        instance = d.pop("instance", UNSET)

        problem = cls(
            title=title,
            status=status,
            type_=type_,
            detail=detail,
            instance=instance,
        )


        problem.additional_properties = d
        return problem

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
