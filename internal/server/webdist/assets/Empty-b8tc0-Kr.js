import{p as k,d as S,af as w,E as z,s as I,U as m,q as B,z as L,g as o,c as n,D as d,aa as y,A as r,h as v,a9 as b,F as D,G as M,H as P,I as a,M as g}from"./index-WH-fP4ir.js";import{u as T}from"./use-locale-Z4YYQKvb.js";var $={iconSizeTiny:"28px",iconSizeSmall:"34px",iconSizeMedium:"40px",iconSizeLarge:"46px",iconSizeHuge:"52px"};function Z(e){const{textColorDisabled:t,iconColor:i,textColor2:c,fontSizeTiny:p,fontSizeSmall:u,fontSizeMedium:C,fontSizeLarge:f,fontSizeHuge:l}=e;return{...$,fontSizeTiny:p,fontSizeSmall:u,fontSizeMedium:C,fontSizeLarge:f,fontSizeHuge:l,textColor:t,iconColor:i,extraTextColor:c}}const F={name:"Empty",common:k,self:Z};var N=S({name:"Empty",render(){return(()=>{const e=w("15c1a247ae156450");return e[0]||(e[0]=z("svg",{viewBox:"0 0 28 28",fill:"none",xmlns:"http://www.w3.org/2000/svg"},[z("path",{d:"M26 7.5C26 11.0899 23.0899 14 19.5 14C15.9101 14 13 11.0899 13 7.5C13 3.91015 15.9101 1 19.5 1C23.0899 1 26 3.91015 26 7.5ZM16.8536 4.14645C16.6583 3.95118 16.3417 3.95118 16.1464 4.14645C15.9512 4.34171 15.9512 4.65829 16.1464 4.85355L18.7929 7.5L16.1464 10.1464C15.9512 10.3417 15.9512 10.6583 16.1464 10.8536C16.3417 11.0488 16.6583 11.0488 16.8536 10.8536L19.5 8.20711L22.1464 10.8536C22.3417 11.0488 22.6583 11.0488 22.8536 10.8536C23.0488 10.6583 23.0488 10.3417 22.8536 10.1464L20.2071 7.5L22.8536 4.85355C23.0488 4.65829 23.0488 4.34171 22.8536 4.14645C22.6583 3.95118 22.3417 3.95118 22.1464 4.14645L19.5 6.79289L16.8536 4.14645Z",fill:"currentColor"}),z("path",{d:"M25 22.75V12.5991C24.5572 13.0765 24.053 13.4961 23.5 13.8454V16H17.5L17.3982 16.0068C17.0322 16.0565 16.75 16.3703 16.75 16.75C16.75 18.2688 15.5188 19.5 14 19.5C12.4812 19.5 11.25 18.2688 11.25 16.75L11.2432 16.6482C11.1935 16.2822 10.8797 16 10.5 16H4.5V7.25C4.5 6.2835 5.2835 5.5 6.25 5.5H12.2696C12.4146 4.97463 12.6153 4.47237 12.865 4H6.25C4.45507 4 3 5.45507 3 7.25V22.75C3 24.5449 4.45507 26 6.25 26H21.75C23.5449 26 25 24.5449 25 22.75ZM4.5 22.75V17.5H9.81597L9.85751 17.7041C10.2905 19.5919 11.9808 21 14 21L14.215 20.9947C16.2095 20.8953 17.842 19.4209 18.184 17.5H23.5V22.75C23.5 23.7165 22.7165 24.5 21.75 24.5H6.25C5.2835 24.5 4.5 23.7165 4.5 22.75Z",fill:"currentColor"})],-1))})()}}),O=I("empty",`
 display: flex;
 flex-direction: column;
 align-items: center;
 font-size: var(--n-font-size);
`,[m("icon",`
 width: var(--n-icon-size);
 height: var(--n-icon-size);
 font-size: var(--n-icon-size);
 line-height: var(--n-icon-size);
 color: var(--n-icon-color);
 transition:
 color .3s var(--n-bezier);
 `,[B("+",[m("description",`
 margin-top: 8px;
 `)])]),m("description",`
 transition: color .3s var(--n-bezier);
 color: var(--n-text-color);
 `),m("extra",`
 text-align: center;
 transition: color .3s var(--n-bezier);
 margin-top: 12px;
 color: var(--n-extra-text-color);
 `)]);const j={...L.props,description:String,showDescription:{type:Boolean,default:!0},showIcon:{type:Boolean,default:!0},size:{type:String,default:"medium"},renderIcon:Function};var G=S({name:"Empty",props:j,slots:Object,setup(e){const{mergedClsPrefixRef:t,inlineThemeDisabled:i,mergedComponentPropsRef:c}=M(e),p=L("Empty","-empty",O,F,e,t),{localeRef:u}=T("Empty"),C=a(()=>e.description??c?.value?.Empty?.description),f=a(()=>c?.value?.Empty?.renderIcon||(()=>(o(),v(N)))),l=a(()=>{const{size:s}=e,{common:{cubicBezierEaseInOut:x},self:{[g("iconSize",s)]:E,[g("fontSize",s)]:_,textColor:R,iconColor:H,extraTextColor:V}}=p.value;return{"--n-icon-size":E,"--n-font-size":_,"--n-bezier":x,"--n-text-color":R,"--n-icon-color":H,"--n-extra-text-color":V}}),h=i?P("empty",a(()=>{let s="";const{size:x}=e;return s+=x[0],s}),l,e):void 0;return{mergedClsPrefix:t,mergedRenderIcon:f,localizedDescription:a(()=>C.value||u.value.description),cssVars:i?void 0:l,themeClass:h?.themeClass,onRender:h?.onRender}},render(){const{$slots:e,mergedClsPrefix:t,onRender:i}=this;return i?.(),o(),n("div",{class:d([`${t}-empty`,this.themeClass]),style:D(this.cssVars)},[this.showIcon?(o(),n("div",{key:0,class:d(`${t}-empty__icon`)},[e.icon?(o(),n(y,{key:0},[r(()=>e.icon())],64)):(o(),v(b,{key:1,clsPrefix:t},{default:this.mergedRenderIcon},1032,["clsPrefix"]))],2)):r(()=>null),this.showDescription?(o(),n("div",{key:2,class:d(`${t}-empty__description`)},[e.default?(o(),n(y,{key:0},[r(()=>e.default())],64)):(o(),n(y,{key:1},[r(()=>this.localizedDescription)],64))],2)):r(()=>null),e.extra?(o(),n("div",{key:4,class:d(`${t}-empty__extra`)},[r(()=>e.extra())],2)):r(()=>null)],6)}});export{G as E,F as e};
