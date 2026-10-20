import{b4 as ve,gd as ge,ge as me,b6 as I,bX as o,er as Y,b5 as U,bW as s,es as G,d as we,b7 as Q,gf as A,o as b,c as y,a as k,b9 as l,bG as i,X as q,ba as pe,fS as xe,j as H,c0 as ye,f as K,c2 as O,ew as p,p as J,f1 as ke,b8 as Se,ex as Ce,bc as x,bd as E,e_ as d,c5 as Be}from"./index-BKOV7uD3.js";import{u as _e}from"./use-merged-state-0NRHneF4.js";function Re(e){const{primaryColor:c,opacityDisabled:f,borderRadius:n,textColor3:S}=e;return{...ge,iconColor:S,textColor:"white",loadingColor:c,opacityDisabled:f,railColor:"rgba(0, 0, 0, .14)",railColorActive:c,buttonBoxShadow:"0 1px 4px 0 rgba(0, 0, 0, 0.3), inset 0 0 1px 0 rgba(0, 0, 0, 0.05)",buttonColor:"#FFF",railBorderRadiusSmall:n,railBorderRadiusMedium:n,railBorderRadiusLarge:n,buttonBorderRadiusSmall:n,buttonBorderRadiusMedium:n,buttonBorderRadiusLarge:n,boxShadowFocus:`0 0 0 2px ${me(c,{alpha:.2})}`}}const $e={common:ve,self:Re};var ze=I("switch",`
 height: var(--n-height);
 min-width: var(--n-width);
 vertical-align: middle;
 user-select: none;
 -webkit-user-select: none;
 display: inline-flex;
 outline: none;
 justify-content: center;
 align-items: center;
`,[o("children-placeholder",`
 height: var(--n-rail-height);
 display: flex;
 flex-direction: column;
 overflow: hidden;
 pointer-events: none;
 visibility: hidden;
 `),o("rail-placeholder",`
 display: flex;
 flex-wrap: none;
 `),o("button-placeholder",`
 width: calc(1.75 * var(--n-rail-height));
 height: var(--n-rail-height);
 `),I("base-loading",`
 position: absolute;
 top: 50%;
 left: 50%;
 transform: translateX(-50%) translateY(-50%);
 font-size: calc(var(--n-button-width) - 4px);
 color: var(--n-loading-color);
 transition: color .3s var(--n-bezier);
 `,[Y({left:"50%",top:"50%",originalTransform:"translateX(-50%) translateY(-50%)"})]),o("checked, unchecked",`
 transition: color .3s var(--n-bezier);
 color: var(--n-text-color);
 box-sizing: border-box;
 position: absolute;
 white-space: nowrap;
 top: 0;
 bottom: 0;
 display: flex;
 align-items: center;
 line-height: 1;
 `),o("checked",`
 right: 0;
 padding-right: calc(1.25 * var(--n-rail-height) - var(--n-offset));
 `),o("unchecked",`
 left: 0;
 justify-content: flex-end;
 padding-left: calc(1.25 * var(--n-rail-height) - var(--n-offset));
 `),U("&:focus",[o("rail",`
 box-shadow: var(--n-box-shadow-focus);
 `)]),s("round",[o("rail","border-radius: calc(var(--n-rail-height) / 2);",[o("button","border-radius: calc(var(--n-button-height) / 2);")])]),G("disabled",[G("icon",[s("rubber-band",[s("pressed",[o("rail",[o("button","max-width: var(--n-button-width-pressed);")])]),o("rail",[U("&:active",[o("button","max-width: var(--n-button-width-pressed);")])]),s("active",[s("pressed",[o("rail",[o("button","left: calc(100% - var(--n-offset) - var(--n-button-width-pressed));")])]),o("rail",[U("&:active",[o("button","left: calc(100% - var(--n-offset) - var(--n-button-width-pressed));")])])])])])]),s("active",[o("rail",[o("button","left: calc(100% - var(--n-button-width) - var(--n-offset))")])]),o("rail",`
 overflow: hidden;
 height: var(--n-rail-height);
 min-width: var(--n-rail-width);
 border-radius: var(--n-rail-border-radius);
 cursor: pointer;
 position: relative;
 transition:
 opacity .3s var(--n-bezier),
 background .3s var(--n-bezier),
 box-shadow .3s var(--n-bezier);
 background-color: var(--n-rail-color);
 `,[o("button-icon",`
 color: var(--n-icon-color);
 transition: color .3s var(--n-bezier);
 font-size: calc(var(--n-button-height) - 4px);
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 display: flex;
 justify-content: center;
 align-items: center;
 line-height: 1;
 `,[Y()]),o("button",`
 align-items: center; 
 top: var(--n-offset);
 left: var(--n-offset);
 height: var(--n-button-height);
 width: var(--n-button-width-pressed);
 max-width: var(--n-button-width);
 border-radius: var(--n-button-border-radius);
 background-color: var(--n-button-color);
 box-shadow: var(--n-button-box-shadow);
 box-sizing: border-box;
 cursor: inherit;
 content: "";
 position: absolute;
 transition:
 background-color .3s var(--n-bezier),
 left .3s var(--n-bezier),
 opacity .3s var(--n-bezier),
 max-width .3s var(--n-bezier),
 box-shadow .3s var(--n-bezier);
 `)]),s("active",[o("rail","background-color: var(--n-rail-color-active);")]),s("loading",[o("rail",`
 cursor: wait;
 `)]),s("disabled",[o("rail",`
 cursor: not-allowed;
 opacity: .5;
 `)])]);const Ve=["aria-checked","tabindex","onClick","onFocus","onBlur","onKeyup","onKeydown"],Fe={...Q.props,size:String,value:{type:[String,Number,Boolean],default:void 0},loading:Boolean,defaultValue:{type:[String,Number,Boolean],default:!1},disabled:{type:Boolean,default:void 0},round:{type:Boolean,default:!0},"onUpdate:value":[Function,Array],onUpdateValue:[Function,Array],checkedValue:{type:[String,Number,Boolean],default:!0},uncheckedValue:{type:[String,Number,Boolean],default:!1},railStyle:Function,rubberBand:{type:Boolean,default:!0},spinProps:Object,onChange:[Function,Array]};let $;var Ke=we({name:"Switch",props:Fe,slots:Object,setup(e){$===void 0&&(typeof CSS<"u"?typeof CSS.supports<"u"?$=CSS.supports("width","max(1px)"):$=!1:$=!0);const{mergedClsPrefixRef:c,inlineThemeDisabled:f,mergedComponentPropsRef:n}=pe(e),S=Q("Switch","-switch",ze,$e,e,c),v=xe(e,{mergedSize(t){if(e.size!==void 0)return e.size;if(t)return t.mergedSize.value;const w=n?.value?.Switch?.size;return w||"medium"}}),{mergedSizeRef:C,mergedDisabledRef:g}=v,B=H(e.defaultValue),z=Be(e,"value"),m=_e(z,B),V=K(()=>m.value===e.checkedValue),a=H(!1),r=H(!1),_=K(()=>{const{railStyle:t}=e;if(t)return t({focused:r.value,checked:V.value})});function F(t){const{"onUpdate:value":w,onChange:P,onUpdateValue:T}=e,{nTriggerFormInput:W,nTriggerFormChange:D}=v;w&&O(w,t),T&&O(T,t),P&&O(P,t),B.value=t,W(),D()}function Z(){const{nTriggerFormFocus:t}=v;t()}function ee(){const{nTriggerFormBlur:t}=v;t()}function te(){e.loading||g.value||(m.value!==e.checkedValue?F(e.checkedValue):F(e.uncheckedValue))}function oe(){r.value=!0,Z()}function ae(){r.value=!1,ee(),a.value=!1}function ie(t){e.loading||g.value||t.key===" "&&(m.value!==e.checkedValue?F(e.checkedValue):F(e.uncheckedValue),a.value=!1)}function ne(t){e.loading||g.value||t.key===" "&&(t.preventDefault(),a.value=!0)}const L=K(()=>{const{value:t}=C,{self:{opacityDisabled:w,railColor:P,railColorActive:T,buttonBoxShadow:W,buttonColor:D,boxShadowFocus:re,loadingColor:le,textColor:se,iconColor:de,[x("buttonHeight",t)]:u,[x("buttonWidth",t)]:ce,[x("buttonWidthPressed",t)]:ue,[x("railHeight",t)]:h,[x("railWidth",t)]:R,[x("railBorderRadius",t)]:he,[x("buttonBorderRadius",t)]:be},common:{cubicBezierEaseInOut:fe}}=S.value;let M,j,N;return $?(M=`calc((${h} - ${u}) / 2)`,j=`max(${h}, ${u})`,N=`max(${R}, calc(${R} + ${u} - ${h}))`):(M=E((d(h)-d(u))/2),j=E(Math.max(d(h),d(u))),N=d(h)>d(u)?R:E(d(R)+d(u)-d(h))),{"--n-bezier":fe,"--n-button-border-radius":be,"--n-button-box-shadow":W,"--n-button-color":D,"--n-button-width":ce,"--n-button-width-pressed":ue,"--n-button-height":u,"--n-height":j,"--n-offset":M,"--n-opacity-disabled":w,"--n-rail-border-radius":he,"--n-rail-color":P,"--n-rail-color-active":T,"--n-rail-height":h,"--n-rail-width":R,"--n-width":N,"--n-box-shadow-focus":re,"--n-loading-color":le,"--n-text-color":se,"--n-icon-color":de}}),X=f?ye("switch",K(()=>C.value[0]),L,e):void 0;return{handleClick:te,handleBlur:ae,handleFocus:oe,handleKeyup:ie,handleKeydown:ne,mergedRailStyle:_,pressed:a,mergedClsPrefix:c,mergedValue:m,checked:V,mergedDisabled:g,cssVars:f?void 0:L,themeClass:X?.themeClass,onRender:X?.onRender}},render(){const{mergedClsPrefix:e,mergedDisabled:c,checked:f,mergedRailStyle:n,onRender:S,$slots:v}=this;S?.();const{checked:C,unchecked:g,icon:B,"checked-icon":z,"unchecked-icon":m}=v,V=!(A(B)&&A(z)&&A(m));return b(),y("div",{role:"switch","aria-checked":f,class:i([`${e}-switch`,this.themeClass,V&&`${e}-switch--icon`,f&&`${e}-switch--active`,c&&`${e}-switch--disabled`,this.round&&`${e}-switch--round`,this.loading&&`${e}-switch--loading`,this.pressed&&`${e}-switch--pressed`,this.rubberBand&&`${e}-switch--rubber-band`]),tabindex:this.mergedDisabled?void 0:0,style:q(this.cssVars),onClick:this.handleClick,onFocus:this.handleFocus,onBlur:this.handleBlur,onKeyup:this.handleKeyup,onKeydown:this.handleKeydown},[k("div",{class:i(`${e}-switch__rail`),"aria-hidden":"true",style:q(n)},[l(()=>p(C,a=>p(g,r=>a||r?(b(),y("div",{key:4,"aria-hidden":!0,class:i(`${e}-switch__children-placeholder`)},[k("div",{class:i(`${e}-switch__rail-placeholder`)},[k("div",{class:i(`${e}-switch__button-placeholder`)},null,2),l(()=>a)],2),k("div",{class:i(`${e}-switch__rail-placeholder`)},[k("div",{class:i(`${e}-switch__button-placeholder`)},null,2),l(()=>r)],2)],2)):null))),k("div",{class:i(`${e}-switch__button`)},[l(()=>p(B,a=>p(z,r=>p(m,_=>(b(),J(Ce,null,{default:()=>this.loading?(b(),J(ke,Se({key:"loading",clsPrefix:e,strokeWidth:20},this.spinProps),null,16,["clsPrefix"])):this.checked&&(r||a)?(b(),y("div",{class:i(`${e}-switch__button-icon`),key:r?"checked-icon":"icon"},[l(()=>r||a)],2)):!this.checked&&(_||a)?(b(),y("div",{class:i(`${e}-switch__button-icon`),key:_?"unchecked-icon":"icon"},[l(()=>_||a)],2)):null},1024)))))),l(()=>p(C,a=>a&&(b(),y("div",{key:"checked",class:i(`${e}-switch__checked`)},[l(()=>a)],2)))),l(()=>p(g,a=>a&&(b(),y("div",{key:"unchecked",class:i(`${e}-switch__unchecked`)},[l(()=>a)],2))))],2)],6)],46,Ve)}});export{Ke as S};
